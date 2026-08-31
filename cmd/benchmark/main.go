package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
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
	connected     atomic.Int64
	peakConnected atomic.Int64
	dialAttempted atomic.Int64
	dialSucceeded atomic.Int64
	dialFailed    atomic.Int64
	sent          atomic.Int64
	received      atomic.Int64
	expected      atomic.Int64
	frames        atomic.Int64
	errors        atomic.Int64
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
	activeClients := flag.Int("active-clients", -1, "exact number of message-sending clients; -1 uses -active")
	sendInterval := flag.Duration("r", defaultSendInterval, "average message interval for active clients")
	rampInterval := flag.Duration("ramp", defaultRampInterval, "delay between connection attempts; 0 disables ramp-up")
	warmup := flag.Duration("warmup", 2*time.Second, "settling time after all dial attempts finish")
	duration := flag.Duration("d", 0, "message-sending duration; 0 runs until Ctrl+C")
	drain := flag.Duration("drain", 3*time.Second, "time to keep connections open after sending stops")
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
	if *activeClients < -1 || *activeClients > *clients {
		log.Fatal("-active-clients must be -1 or between 0 and -c")
	}
	if *sendInterval <= 0 {
		log.Fatal("-r must be greater than 0")
	}
	if *rampInterval < 0 {
		log.Fatal("-ramp cannot be negative")
	}
	if *warmup < 0 || *drain < 0 {
		log.Fatal("-warmup and -drain cannot be negative")
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

	activeClientCount := resolveActiveClientCount(*clients, *activeRatio, *activeClients)
	if *sessionID == "" && activeClientCount > 0 {
		log.Printf("[WARN] -sid is empty; switching to listener-only mode")
		activeClientCount = 0
	}

	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	clientCtx, cancelClients := context.WithCancel(signalCtx)
	defer cancelClients()

	var stats metrics
	startedAt := time.Now()
	go monitor(clientCtx, &stats, startedAt, *reportInterval)

	log.Printf(
		"[BENCHMARK] starting clients=%d room=%d active_clients=%d send_interval=%s ramp=%s warmup=%s send_duration=%s drain=%s targets=%v",
		*clients,
		*roomID,
		activeClientCount,
		*sendInterval,
		*rampInterval,
		*warmup,
		*duration,
		*drain,
		targets,
	)

	startSending := make(chan struct{})
	stopSending := make(chan struct{})
	var clientWG sync.WaitGroup
	var dialWG sync.WaitGroup
	var rampTicker *time.Ticker
	if *rampInterval > 0 {
		rampTicker = time.NewTicker(*rampInterval)
		defer rampTicker.Stop()
	}

launchLoop:
	for id := 0; id < *clients; id++ {
		if rampTicker != nil {
			select {
			case <-signalCtx.Done():
				break launchLoop
			case <-rampTicker.C:
			}
		}

		clientWG.Add(1)
		dialWG.Add(1)
		go func(clientID int) {
			defer clientWG.Done()
			target := targets[clientID%len(targets)]
			runClient(
				clientCtx,
				&stats,
				target,
				*origin,
				*sessionID,
				*roomID,
				clientID,
				isActiveClient(clientID, *clients, activeClientCount),
				*sendInterval,
				startSending,
				stopSending,
				&dialWG,
			)
		}(id)
	}

	dialWG.Wait()
	if signalCtx.Err() == nil && waitFor(signalCtx, *warmup) {
		log.Printf("[BENCHMARK] warm-up complete; starting message phase")
		close(startSending)
		if *duration > 0 {
			if waitFor(signalCtx, *duration) {
				close(stopSending)
				log.Printf("[BENCHMARK] sending stopped; draining broadcasts for %s", *drain)
				_ = waitFor(signalCtx, *drain)
			}
		} else {
			<-signalCtx.Done()
		}
	}
	cancelClients()
	clientWG.Wait()
	printFinal(&stats, startedAt)
}

func resolveActiveClientCount(total int, ratio float64, override int) int {
	if override >= 0 {
		return override
	}
	count := int(math.Round(float64(total) * ratio))
	if ratio > 0 && count == 0 {
		count = 1
	}
	if count > total {
		return total
	}
	return count
}

func isActiveClient(clientID, total, activeCount int) bool {
	if total <= 0 || activeCount <= 0 {
		return false
	}
	before := int64(clientID) * int64(activeCount) / int64(total)
	after := int64(clientID+1) * int64(activeCount) / int64(total)
	return after > before
}

func waitFor(ctx context.Context, duration time.Duration) bool {
	if duration == 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
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
	isTalker bool,
	sendInterval time.Duration,
	startSending <-chan struct{},
	stopSending <-chan struct{},
	dialWG *sync.WaitGroup,
) {
	stats.dialAttempted.Add(1)
	endpoint, err := buildWebSocketURL(target, roomID)
	if err != nil {
		recordDialFailure(stats, clientID, nil, err)
		dialWG.Done()
		return
	}

	headers := http.Header{}
	if origin != "" {
		headers.Set("Origin", origin)
	}
	if isTalker && sessionID != "" {
		headers.Set("Cookie", "sid="+sessionID)
	}

	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second
	conn, response, err := dialer.DialContext(ctx, endpoint, headers)
	if err != nil {
		recordDialFailure(stats, clientID, response, err)
		dialWG.Done()
		return
	}
	stats.dialSucceeded.Add(1)
	connected := stats.connected.Add(1)
	updatePeak(&stats.peakConnected, connected)
	dialWG.Done()
	defer func() {
		_ = conn.Close()
		stats.connected.Add(-1)
	}()

	readDone := make(chan struct{})
	go readMessages(ctx, conn, stats, readDone)

	if !isTalker {
		select {
		case <-ctx.Done():
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "benchmark stopped"), time.Now().Add(time.Second))
		case <-readDone:
		}
		return
	}

	select {
	case <-ctx.Done():
		return
	case <-readDone:
		return
	case <-startSending:
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(clientID)))
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
		case <-stopSending:
			timer.Stop()
			select {
			case <-ctx.Done():
			case <-readDone:
			}
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

func recordDialFailure(stats *metrics, clientID int, response *http.Response, err error) {
	stats.errors.Add(1)
	failureNumber := stats.dialFailed.Add(1)
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if failureNumber > 10 {
		return
	}
	if response != nil {
		log.Printf("[DIAL ERROR %d/10] client=%d status=%s err=%v", failureNumber, clientID, response.Status, err)
		return
	}
	log.Printf("[DIAL ERROR %d/10] client=%d err=%v", failureNumber, clientID, err)
}

func updatePeak(peak *atomic.Int64, current int64) {
	for {
		previous := peak.Load()
		if current <= previous || peak.CompareAndSwap(previous, current) {
			return
		}
	}
}

func readMessages(ctx context.Context, conn *websocket.Conn, stats *metrics, done chan<- struct{}) {
	defer close(done)
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() == nil && !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
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
				"[STATS] elapsed=%s conns=%d peak=%d dials=%d/%d sent/s=%d recv/s=%d frames=%d lag=%d loss=%.2f%% errors=%d",
				time.Since(startedAt).Round(time.Second),
				stats.connected.Load(),
				stats.peakConnected.Load(),
				stats.dialSucceeded.Load(),
				stats.dialAttempted.Load(),
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
		"[FINAL] duration=%s conns=%d sent=%d received=%d frames=%d expected=%d errors=%d peak_conns=%d dial_attempted=%d dial_succeeded=%d dial_failed=%d",
		duration.Round(time.Millisecond),
		stats.connected.Load(),
		stats.sent.Load(),
		stats.received.Load(),
		stats.frames.Load(),
		stats.expected.Load(),
		stats.errors.Load(),
		stats.peakConnected.Load(),
		stats.dialAttempted.Load(),
		stats.dialSucceeded.Load(),
		stats.dialFailed.Load(),
	)
}
