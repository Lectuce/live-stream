package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

const (
	defaultClients        = 30000
	defaultSendInterval   = time.Second
	defaultActiveRatio    = 0.001
	defaultRampInterval   = time.Millisecond
	defaultReportInterval = time.Second
)

type metrics struct {
	connected atomic.Int64
	sent      atomic.Int64
	received  atomic.Int64
	expected  atomic.Int64
	frames    atomic.Int64
	errors    atomic.Int64
}

type incomingMessage struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

type outgoingMessage struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

func main() {
	clients := flag.Int("c", defaultClients, "number of concurrent WebSocket clients")
	urlsFlag := flag.String("urls", "ws://localhost:9000/api/v1/ws", "comma-separated WebSocket endpoint URLs")
	roomID := flag.Uint64("room", 1, "existing live room ID")
	origin := flag.String("origin", "http://localhost:5173", "Origin header allowed by the server")
	sessionID := flag.String("sid", "", "optional sid cookie; required for bots that send messages")
	activeRatio := flag.Float64("active", defaultActiveRatio, "fraction of connected clients that send messages (0..1)")
	sendInterval := flag.Duration("r", defaultSendInterval, "average message interval for active clients")
	rampInterval := flag.Duration("ramp", defaultRampInterval, "delay between connection attempts; 0 disables ramp-up")
	duration := flag.Duration("d", 0, "test duration; 0 runs until Ctrl+C")
	reportInterval := flag.Duration("report", defaultReportInterval, "metrics reporting interval")
	flag.Parse()

	if *clients <= 0 {
		log.Fatal("-c must be greater than 0")
	}
	if *roomID == 0 {
		log.Fatal("-room must be greater than 0")
	}
	if *activeRatio < 0 || *activeRatio > 1 {
		log.Fatal("-active must be between 0 and 1")
	}
	if *sendInterval <= 0 {
		log.Fatal("-r must be greater than 0")
	}
	if *rampInterval < 0 {
		log.Fatal("-ramp cannot be negative")
	}
	if *reportInterval <= 0 {
		log.Fatal("-report must be greater than 0")
	}

	targets := splitTargets(*urlsFlag)
	if len(targets) == 0 {
		log.Fatal("-urls must contain at least one WebSocket URL")
	}
	for _, target := range targets {
		if _, err := buildWebSocketURL(target, *roomID); err != nil {
			log.Fatalf("invalid target URL %q: %v", target, err)
		}
	}

	effectiveActiveRatio := *activeRatio
	if *sessionID == "" && effectiveActiveRatio > 0 {
		log.Printf("[WARN] -sid is empty; switching to listener-only mode (-active=0)")
		effectiveActiveRatio = 0
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *duration)
		defer cancel()
	}

	var stats metrics
	startedAt := time.Now()
	go monitor(ctx, &stats, startedAt, *reportInterval)

	log.Printf(
		"[BENCHMARK] starting clients=%d room=%d active_ratio=%.4f send_interval=%s ramp=%s targets=%v",
		*clients,
		*roomID,
		effectiveActiveRatio,
		*sendInterval,
		*rampInterval,
		targets,
	)

	var wg sync.WaitGroup
	var rampTicker *time.Ticker
	if *rampInterval > 0 {
		rampTicker = time.NewTicker(*rampInterval)
		defer rampTicker.Stop()
	}

launchLoop:
	for id := 0; id < *clients; id++ {
		if rampTicker != nil {
			select {
			case <-ctx.Done():
				break launchLoop
			case <-rampTicker.C:
			}
		}

		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()
			target := targets[clientID%len(targets)]
			runClient(
				ctx,
				&stats,
				target,
				*origin,
				*sessionID,
				*roomID,
				clientID,
				effectiveActiveRatio,
				*sendInterval,
			)
		}(id)
	}

	wg.Wait()
	printFinal(&stats, startedAt)
}

func splitTargets(raw string) []string {
	parts := strings.Split(raw, ",")
	targets := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			targets = append(targets, part)
		}
	}
	return targets
}

func buildWebSocketURL(raw string, roomID uint64) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return "", fmt.Errorf("scheme must be ws or wss")
	}
	if u.Host == "" {
		return "", fmt.Errorf("host is required")
	}
	query := u.Query()
	query.Set("room_id", fmt.Sprintf("%d", roomID))
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func runClient(
	ctx context.Context,
	stats *metrics,
	target string,
	origin string,
	sessionID string,
	roomID uint64,
	clientID int,
	activeRatio float64,
	sendInterval time.Duration,
) {
	endpoint, err := buildWebSocketURL(target, roomID)
	if err != nil {
		stats.errors.Add(1)
		return
	}

	headers := http.Header{}
	if origin != "" {
		headers.Set("Origin", origin)
	}
	if sessionID != "" {
		headers.Set("Cookie", "sid="+sessionID)
	}

	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second
	conn, response, err := dialer.DialContext(ctx, endpoint, headers)
	if err != nil {
		stats.errors.Add(1)
		if response != nil && clientID < 10 {
			log.Printf("[DIAL ERROR] client=%d status=%s err=%v", clientID, response.Status, err)
		} else if clientID < 10 {
			log.Printf("[DIAL ERROR] client=%d err=%v", clientID, err)
		}
		return
	}
	stats.connected.Add(1)
	defer func() {
		_ = conn.Close()
		stats.connected.Add(-1)
	}()

	readDone := make(chan struct{})
	go readMessages(ctx, conn, stats, readDone)

	rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(clientID)))
	isTalker := sessionID != "" && rng.Float64() < activeRatio
	if !isTalker {
		select {
		case <-ctx.Done():
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "benchmark stopped"), time.Now().Add(time.Second))
		case <-readDone:
		}
		return
	}

	sequence := int64(0)
	for {
		jitteredInterval := time.Duration(float64(sendInterval) * (0.5 + rng.Float64()))
		timer := time.NewTimer(jitteredInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "benchmark stopped"), time.Now().Add(time.Second))
			return
		case <-readDone:
			timer.Stop()
			return
		case <-timer.C:
			sequence++
			message := outgoingMessage{
				Type:    "danma",
				Content: fmt.Sprintf("bench-%d-%d", clientID, sequence),
			}
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteJSON(message); err != nil {
				stats.errors.Add(1)
				return
			}
			stats.sent.Add(1)
			stats.expected.Add(stats.connected.Load())
		}
	}
}

func readMessages(ctx context.Context, conn *websocket.Conn, stats *metrics, done chan<- struct{}) {
	defer close(done)
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() == nil {
				stats.errors.Add(1)
			}
			return
		}
		stats.frames.Add(1)

		var message incomingMessage
		if err := json.Unmarshal(payload, &message); err != nil {
			stats.errors.Add(1)
			continue
		}
		// Redis currently broadcasts persisted danmaku without a type field,
		// so content is also accepted as the signal for a received danmaku.
		if message.Type == "danma" || message.Content != "" {
			stats.received.Add(1)
		}
	}
}

func monitor(ctx context.Context, stats *metrics, startedAt time.Time, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastSent int64
	var lastReceived int64
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sent := stats.sent.Load()
			received := stats.received.Load()
			expected := stats.expected.Load()
			lag := expected - received
			loss := 0.0
			if expected > 0 {
				loss = float64(lag) / float64(expected) * 100
			}
			log.Printf(
				"[STATS] elapsed=%s conns=%d sent/s=%d recv/s=%d frames=%d lag=%d loss=%.2f%% errors=%d",
				time.Since(startedAt).Round(time.Second),
				stats.connected.Load(),
				sent-lastSent,
				received-lastReceived,
				stats.frames.Load(),
				lag,
				loss,
				stats.errors.Load(),
			)
			lastSent = sent
			lastReceived = received
		}
	}
}

func printFinal(stats *metrics, startedAt time.Time) {
	duration := time.Since(startedAt)
	log.Printf(
		"[FINAL] duration=%s conns=%d sent=%d received=%d frames=%d expected=%d errors=%d",
		duration.Round(time.Millisecond),
		stats.connected.Load(),
		stats.sent.Load(),
		stats.received.Load(),
		stats.frames.Load(),
		stats.expected.Load(),
		stats.errors.Load(),
	)
}
