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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"
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
	connected       atomic.Int64
	peakConnected   atomic.Int64
	audience        atomic.Int64
	unexpectedExit  atomic.Int64
	dialAttempted   atomic.Int64
	dialSucceeded   atomic.Int64
	dialFailed      atomic.Int64
	readFailed      atomic.Int64
	sent            atomic.Int64
	accepted        atomic.Int64
	rejected        atomic.Int64
	realtime        atomic.Int64
	received        atomic.Int64
	expected        atomic.Int64
	frames          atomic.Int64
	errors          atomic.Int64
	ackSent         atomic.Int64
	resyncRequired  atomic.Int64
	recovered       atomic.Int64
	latency         latencyHistogram
	realtimeLatency latencyHistogram
	recoveryLatency latencyHistogram
	sentIDs         sync.Map
	terminalIDs     sync.Map
}

func (m *metrics) registerSent(messageID string) {
	if messageID != "" {
		m.sentIDs.Store(messageID, struct{}{})
	}
}

func (m *metrics) unregisterSent(messageID string) {
	if messageID != "" {
		m.sentIDs.Delete(messageID)
	}
}

func (m *metrics) wasSent(messageID string) bool {
	if messageID == "" {
		return false
	}
	_, ok := m.sentIDs.Load(messageID)
	return ok
}

func (m *metrics) recordTerminal(messageID, status string) {
	if !m.wasSent(messageID) {
		return
	}
	if _, loaded := m.terminalIDs.LoadOrStore(messageID, status); loaded {
		return
	}
	switch status {
	case "accepted":
		m.accepted.Add(1)
		m.expected.Add(m.audience.Load())
	case "rejected":
		m.rejected.Add(1)
	}
}

var latencyBucketUpperBounds = [...]time.Duration{
	time.Millisecond,
	5 * time.Millisecond,
	10 * time.Millisecond,
	25 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	250 * time.Millisecond,
	500 * time.Millisecond,
	750 * time.Millisecond,
	time.Second,
	1500 * time.Millisecond,
	2 * time.Second,
	3 * time.Second,
	4 * time.Second,
	5 * time.Second,
	10 * time.Second,
}

type latencyHistogram struct {
	count   atomic.Uint64
	buckets [len(latencyBucketUpperBounds) + 1]atomic.Uint64
}

func (h *latencyHistogram) observe(latency time.Duration) {
	if latency < 0 {
		return
	}
	h.count.Add(1)
	for i, upperBound := range latencyBucketUpperBounds {
		if latency <= upperBound {
			h.buckets[i].Add(1)
			return
		}
	}
	h.buckets[len(h.buckets)-1].Add(1)
}

func (h *latencyHistogram) percentile(percentile float64) time.Duration {
	count := h.count.Load()
	if count == 0 {
		return 0
	}
	target := uint64(math.Ceil(float64(count) * percentile))
	var cumulative uint64
	for i, upperBound := range latencyBucketUpperBounds {
		cumulative += h.buckets[i].Load()
		if cumulative >= target {
			return upperBound
		}
	}
	return latencyBucketUpperBounds[len(latencyBucketUpperBounds)-1]
}

type incomingMessage struct {
	Type      string         `json:"type"`
	MessageID string         `json:"message_id"`
	Content   string         `json:"content"`
	RoomSeq   uint64         `json:"room_seq"`
	AfterSeq  uint64         `json:"after_seq"`
	LatestSeq uint64         `json:"latest_seq"`
	Data      map[string]any `json:"data"`
}

type replayResponse struct {
	Data struct {
		Items   []incomingMessage `json:"items"`
		NextSeq uint64            `json:"next_seq"`
		HasMore bool              `json:"has_more"`
	} `json:"data"`
}

type deliveryTracker struct {
	mu      sync.Mutex
	seen    map[string]struct{}
	order   []string
	lastSeq uint64
}

func newDeliveryTracker() *deliveryTracker {
	return &deliveryTracker{seen: make(map[string]struct{}), order: make([]string, 0, 8192)}
}

func (t *deliveryTracker) observe(message incomingMessage) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if message.MessageID != "" {
		if _, exists := t.seen[message.MessageID]; exists {
			return false
		}
		t.seen[message.MessageID] = struct{}{}
		t.order = append(t.order, message.MessageID)
		if len(t.order) > 8192 {
			delete(t.seen, t.order[0])
			t.order = t.order[1:]
		}
	}
	if message.RoomSeq > t.lastSeq {
		t.lastSeq = message.RoomSeq
	}
	return true
}

func (t *deliveryTracker) cursor() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastSeq
}

type outgoingMessage struct {
	Type      string `json:"type"`
	MessageID string `json:"message_id,omitempty"`
	RoomID    uint64 `json:"room_id,omitempty"`
	AckSeq    uint64 `json:"ack_seq,omitempty"`
	Content   string `json:"content,omitempty"`
}

func main() {
	clients := flag.Int("c", defaultClients, "number of concurrent WebSocket clients")
	urlsFlag := flag.String("urls", "ws://localhost:9000/api/v1/ws", "comma-separated WebSocket endpoint URLs")
	roomID := flag.Uint64("room", 1, "existing live room ID")
	origin := flag.String("origin", "http://localhost:5173", "Origin header allowed by the server")
	sessionID := flag.String("sid", "", "optional sid cookie; required for bots that send messages")
	sessionIDsFile := flag.String("sids-file", "", "optional newline-delimited sid file; assigns one distinct session to each message-sending client")
	distinctTalkerIPs := flag.Bool("distinct-talker-ips", false, "send a distinct X-Forwarded-For address for each message-sending client")
	activeRatio := flag.Float64("active", defaultActiveRatio, "fraction of connected clients that send messages (0..1)")
	activeClients := flag.Int("active-clients", -1, "exact number of message-sending clients; -1 uses -active")
	sendInterval := flag.Duration("r", defaultSendInterval, "average message interval for active clients")
	rampInterval := flag.Duration("ramp", defaultRampInterval, "delay between connection attempts; 0 disables ramp-up")
	warmup := flag.Duration("warmup", 2*time.Second, "settling time after all dial attempts finish")
	duration := flag.Duration("d", 0, "message-sending duration; 0 runs until Ctrl+C")
	drain := flag.Duration("drain", 3*time.Second, "time to keep connections open after sending stops")
	reportInterval := flag.Duration("report", defaultReportInterval, "metrics reporting interval")
	ackEvery := flag.Int("ack-every", 0, "send a cumulative ACK after this many danmaku deliveries per client; 0 disables ACK")
	replayBaseURL := flag.String("replay-base-url", "", "HTTP API base URL used for automatic gap replay; empty derives it from -urls")
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
	if *ackEvery < 0 {
		log.Fatal("-ack-every cannot be negative")
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
	if *sessionID != "" && *sessionIDsFile != "" {
		log.Fatal("-sid and -sids-file cannot be used together")
	}
	sessionIDs, err := loadSessionIDs(*sessionIDsFile)
	if err != nil {
		log.Fatalf("load -sids-file: %v", err)
	}
	if len(sessionIDs) > 0 && len(sessionIDs) < activeClientCount {
		log.Fatalf("-sids-file contains %d sessions, need at least %d", len(sessionIDs), activeClientCount)
	}
	if *sessionID == "" && len(sessionIDs) == 0 && activeClientCount > 0 {
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
		"[BENCHMARK] starting clients=%d room=%d active_clients=%d send_interval=%s ramp=%s warmup=%s send_duration=%s drain=%s ack_every=%d targets=%v",
		*clients,
		*roomID,
		activeClientCount,
		*sendInterval,
		*rampInterval,
		*warmup,
		*duration,
		*drain,
		*ackEvery,
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
			talkerOrdinal, isTalker := activeClientOrdinal(clientID, *clients, activeClientCount)
			clientSessionID := *sessionID
			forwardedFor := ""
			if isTalker && len(sessionIDs) > 0 {
				clientSessionID = sessionIDs[talkerOrdinal]
			}
			if isTalker && *distinctTalkerIPs {
				forwardedFor = benchmarkClientIP(talkerOrdinal)
			}
			runClient(
				clientCtx,
				&stats,
				target,
				*origin,
				clientSessionID,
				forwardedFor,
				*roomID,
				clientID,
				isTalker,
				*sendInterval,
				*ackEvery,
				*replayBaseURL,
				startSending,
				stopSending,
				&dialWG,
			)
		}(id)
	}

	dialWG.Wait()
	if signalCtx.Err() == nil && waitFor(signalCtx, *warmup) {
		stats.audience.Store(stats.connected.Load())
		log.Printf(
			"[BENCHMARK] warm-up complete; starting message phase with frozen audience=%d",
			stats.audience.Load(),
		)
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

func activeClientOrdinal(clientID, total, activeCount int) (int, bool) {
	if total <= 0 || activeCount <= 0 {
		return 0, false
	}
	before := int64(clientID) * int64(activeCount) / int64(total)
	after := int64(clientID+1) * int64(activeCount) / int64(total)
	if after <= before {
		return 0, false
	}
	return int(after - 1), true
}

func loadSessionIDs(path string) ([]string, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(contents), "\n")
	sessionIDs := make([]string, 0, len(lines))
	for lineNumber, line := range lines {
		sessionID := strings.TrimSpace(line)
		if sessionID == "" {
			continue
		}
		if len(sessionID) != 64 {
			return nil, fmt.Errorf("line %d has invalid sid length %d", lineNumber+1, len(sessionID))
		}
		sessionIDs = append(sessionIDs, sessionID)
	}
	return sessionIDs, nil
}

func benchmarkClientIP(ordinal int) string {
	// 198.18.0.0/15 is reserved for benchmark traffic. Avoid network and
	// broadcast-looking addresses so logs remain easy to inspect.
	value := ordinal + 1
	second := 18 + (value/(256*254))%2
	third := (value / 254) % 256
	fourth := value%254 + 1
	return fmt.Sprintf("198.%d.%d.%d", second, third, fourth)
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
	forwardedFor string,
	roomID uint64,
	clientID int,
	isTalker bool,
	sendInterval time.Duration,
	ackEvery int,
	replayBaseURL string,
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
	if forwardedFor != "" {
		headers.Set("X-Forwarded-For", forwardedFor)
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
		if ctx.Err() == nil {
			stats.unexpectedExit.Add(1)
		}
		_ = conn.Close()
		stats.connected.Add(-1)
	}()

	var writeMu sync.Mutex
	readDone := make(chan struct{})
	go readMessages(ctx, conn, stats, readDone, &writeMu, roomID, clientID, ackEvery, replayBaseURL, target)

	if !isTalker {
		select {
		case <-ctx.Done():
			writeCloseControl(conn, &writeMu)
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
			writeCloseControl(conn, &writeMu)
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
				Type:      "danma",
				MessageID: uuid.NewString(),
				Content:   fmt.Sprintf("bench-%d-%d-%d", clientID, sequence, time.Now().UnixNano()),
			}
			stats.registerSent(message.MessageID)
			if err := writeJSON(conn, &writeMu, message); err != nil {
				stats.unregisterSent(message.MessageID)
				stats.errors.Add(1)
				return
			}
			stats.sent.Add(1)
		}
	}
}

func writeJSON(conn *websocket.Conn, mu *sync.Mutex, value any) error {
	mu.Lock()
	defer mu.Unlock()
	if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	return conn.WriteJSON(value)
}

func writeCloseControl(conn *websocket.Conn, mu *sync.Mutex) {
	mu.Lock()
	defer mu.Unlock()
	_ = conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "benchmark stopped"),
		time.Now().Add(time.Second),
	)
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

func readMessages(
	ctx context.Context,
	conn *websocket.Conn,
	stats *metrics,
	done chan<- struct{},
	writeMu *sync.Mutex,
	roomID uint64,
	clientID int,
	ackEvery int,
	replayBaseURL string,
	webSocketTarget string,
) {
	defer close(done)
	readCtx, cancelRead := context.WithCancel(ctx)
	tracker := newDeliveryTracker()
	var recoveryRunning atomic.Bool
	var sampledMode atomic.Bool
	var sampledGeneration atomic.Uint64
	var pullCursor atomic.Uint64
	var replayMu sync.Mutex
	var recoveryWG sync.WaitGroup
	defer func() {
		cancelRead()
		sampledMode.Store(false)
		recoveryWG.Wait()
	}()
	deliveriesSinceAck := 0
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() == nil && !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				stats.errors.Add(1)
				failureNumber := stats.readFailed.Add(1)
				if failureNumber <= 20 {
					log.Printf(
						"[READ ERROR %d/20] client=%d target=%s err=%v",
						failureNumber,
						clientID,
						webSocketTarget,
						err,
					)
				}
			}
			return
		}
		stats.frames.Add(1)

		var message incomingMessage
		if err := json.Unmarshal(payload, &message); err != nil {
			stats.errors.Add(1)
			continue
		}
		if message.Type == "accepted" {
			stats.recordTerminal(message.MessageID, "accepted")
			continue
		}
		if message.Type == "error" && message.MessageID != "" {
			stats.recordTerminal(message.MessageID, "rejected")
			continue
		}
		if message.Type == "resync_required" {
			stats.resyncRequired.Add(1)
			if recoveryRunning.CompareAndSwap(false, true) {
				recoveryWG.Add(1)
				go func(afterSeq, latestSeq uint64) {
					defer recoveryWG.Done()
					defer recoveryRunning.Store(false)
					replayMu.Lock()
					_, err := recoverReplay(
						readCtx,
						stats,
						tracker,
						replayBaseURL,
						webSocketTarget,
						roomID,
						afterSeq,
						latestSeq,
					)
					replayMu.Unlock()
					if err != nil && readCtx.Err() == nil {
						log.Printf("[REPLAY ERROR] room=%d after_seq=%d: %v", roomID, afterSeq, err)
						stats.errors.Add(1)
					}
				}(message.AfterSeq, message.LatestSeq)
			}
			continue
		}
		if message.Type == "system" && message.Data["sub_type"] == "delivery_policy" {
			mode, _ := message.Data["delivery_mode"].(string)
			if mode == "sampled" && sampledMode.CompareAndSwap(false, true) {
				generation := sampledGeneration.Add(1)
				cursor := tracker.cursor()
				if cursor == 0 && message.RoomSeq > 0 {
					cursor = message.RoomSeq
				}
				pullCursor.Store(cursor)
				pollInterval := time.Second
				if raw, ok := message.Data["poll_interval_ms"].(float64); ok && raw > 0 {
					pollInterval = time.Duration(raw) * time.Millisecond
				}
				recoveryWG.Add(1)
				go func() {
					defer recoveryWG.Done()
					ticker := time.NewTicker(pollInterval)
					defer ticker.Stop()
					for sampledMode.Load() && sampledGeneration.Load() == generation {
						select {
						case <-readCtx.Done():
							return
						case <-ticker.C:
							replayMu.Lock()
							next, err := recoverReplay(
								readCtx,
								stats,
								tracker,
								replayBaseURL,
								webSocketTarget,
								roomID,
								pullCursor.Load(),
								0,
							)
							replayMu.Unlock()
							if next > pullCursor.Load() {
								pullCursor.Store(next)
							}
							if err != nil && readCtx.Err() == nil {
								log.Printf("[SAMPLED REPLAY ERROR] room=%d: %v", roomID, err)
								stats.errors.Add(1)
								return
							}
						}
					}
				}()
			} else if mode == "push" && sampledMode.Swap(false) {
				sampledGeneration.Add(1)
				recoveryWG.Add(1)
				go func(targetSeq uint64) {
					defer recoveryWG.Done()
					replayMu.Lock()
					next, err := recoverReplay(
						readCtx,
						stats,
						tracker,
						replayBaseURL,
						webSocketTarget,
						roomID,
						pullCursor.Load(),
						targetSeq,
					)
					replayMu.Unlock()
					if next > pullCursor.Load() {
						pullCursor.Store(next)
					}
					if err != nil && readCtx.Err() == nil {
						log.Printf("[PUSH RECOVERY ERROR] room=%d: %v", roomID, err)
						stats.errors.Add(1)
					}
				}(message.RoomSeq)
			}
			continue
		}
		// 只统计本轮压测实际发送的 message_id。复用已有直播间时，其他
		// 用户或上一轮残留的 bench-* 消息不能污染本轮投递结果。
		if message.Type == "danma" || message.Content != "" {
			if !stats.wasSent(message.MessageID) {
				continue
			}
			if !tracker.observe(message) {
				continue
			}
			stats.realtime.Add(1)
			stats.received.Add(1)
			if sentAt, ok := benchmarkSentAt(message.Content); ok {
				latency := time.Since(sentAt)
				stats.latency.observe(latency)
				stats.realtimeLatency.observe(latency)
			}
			if message.RoomSeq > 0 {
				// room_seq 是 Kafka 分区游标。它在单个房间内单调递增，
				// 但当其他房间共享该分区时可能出现间隔。
				deliveriesSinceAck++
				if ackEvery > 0 && deliveriesSinceAck >= ackEvery {
					ack := outgoingMessage{Type: "ack", RoomID: roomID, AckSeq: message.RoomSeq}
					if err := writeJSON(conn, writeMu, ack); err != nil {
						if ctx.Err() == nil {
							stats.errors.Add(1)
						}
						return
					}
					stats.ackSent.Add(1)
					deliveriesSinceAck = 0
				}
			}
		}
	}
}

func recoverReplay(
	ctx context.Context,
	stats *metrics,
	tracker *deliveryTracker,
	configuredBaseURL string,
	webSocketTarget string,
	roomID uint64,
	afterSeq uint64,
	latestSeq uint64,
) (uint64, error) {
	baseURL, err := replayHTTPBaseURL(configuredBaseURL, webSocketTarget)
	if err != nil {
		return afterSeq, err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	cursor := afterSeq
	for page := 0; page < 100; page++ {
		endpoint := fmt.Sprintf(
			"%s/api/v1/rooms/%d/messages/replay?after_seq=%d&limit=500",
			strings.TrimRight(baseURL, "/"),
			roomID,
			cursor,
		)
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return cursor, err
		}
		response, err := client.Do(request)
		if err != nil {
			return cursor, err
		}
		var payload replayResponse
		decodeErr := json.NewDecoder(response.Body).Decode(&payload)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return cursor, fmt.Errorf("replay status %s", response.Status)
		}
		if decodeErr != nil {
			return cursor, decodeErr
		}
		for _, message := range payload.Data.Items {
			message.Type = "danma"
			if !stats.wasSent(message.MessageID) {
				continue
			}
			if !tracker.observe(message) {
				continue
			}
			stats.received.Add(1)
			stats.recovered.Add(1)
			if sentAt, ok := benchmarkSentAt(message.Content); ok {
				latency := time.Since(sentAt)
				stats.latency.observe(latency)
				stats.recoveryLatency.observe(latency)
			}
		}
		if payload.Data.NextSeq > cursor {
			cursor = payload.Data.NextSeq
		}
		if !payload.Data.HasMore {
			if latestSeq == 0 || cursor >= latestSeq {
				return cursor, nil
			}
			select {
			case <-ctx.Done():
				return cursor, ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	return cursor, fmt.Errorf("replay exceeded page limit")
}

func replayHTTPBaseURL(configured, webSocketTarget string) (string, error) {
	if strings.TrimSpace(configured) != "" {
		u, err := url.Parse(configured)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return "", fmt.Errorf("invalid replay base URL %q", configured)
		}
		return strings.TrimRight(configured, "/"), nil
	}
	u, err := url.Parse(webSocketTarget)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("derive replay URL from %q", webSocketTarget)
	}
	if u.Scheme == "wss" {
		u.Scheme = "https"
	} else {
		u.Scheme = "http"
	}
	u.Path = ""
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
}

func benchmarkSentAt(content string) (time.Time, bool) {
	if !strings.HasPrefix(content, "bench-") || strings.Count(content, "-") != 3 {
		return time.Time{}, false
	}
	separator := strings.LastIndexByte(content, '-')
	if separator < len("bench-") || separator == len(content)-1 {
		return time.Time{}, false
	}
	nanoseconds, err := strconv.ParseInt(content[separator+1:], 10, 64)
	if err != nil || nanoseconds <= 0 {
		return time.Time{}, false
	}
	return time.Unix(0, nanoseconds), true
}

func monitor(ctx context.Context, stats *metrics, startedAt time.Time, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastSent int64
	var lastReceived int64
	var lastAckSent int64
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sent := stats.sent.Load()
			accepted := stats.accepted.Load()
			rejected := stats.rejected.Load()
			realtime := stats.realtime.Load()
			received := stats.received.Load()
			ackSent := stats.ackSent.Load()
			expected := stats.expected.Load()
			lag := expected - received
			loss := 0.0
			if expected > 0 {
				loss = float64(lag) / float64(expected) * 100
			}
			log.Printf(
				"[STATS] elapsed=%s conns=%d peak=%d dials=%d/%d sent_delta=%d accepted=%d rejected=%d realtime=%d recv_delta=%d recovered=%d ack_delta=%d frames=%d lag=%d loss=%.2f%% errors=%d read_failed=%d latency_p50=%s latency_p95=%s latency_p99=%s",
				time.Since(startedAt).Round(time.Second),
				stats.connected.Load(),
				stats.peakConnected.Load(),
				stats.dialSucceeded.Load(),
				stats.dialAttempted.Load(),
				sent-lastSent,
				accepted,
				rejected,
				realtime,
				received-lastReceived,
				stats.recovered.Load(),
				ackSent-lastAckSent,
				stats.frames.Load(),
				lag,
				loss,
				stats.errors.Load(),
				stats.readFailed.Load(),
				stats.latency.percentile(0.50),
				stats.latency.percentile(0.95),
				stats.latency.percentile(0.99),
			)
			lastSent = sent
			lastReceived = received
			lastAckSent = ackSent
		}
	}
}

func printFinal(stats *metrics, startedAt time.Time) {
	duration := time.Since(startedAt)
	log.Printf(
		"[FINAL] duration=%s conns=%d audience=%d sent=%d accepted=%d rejected=%d realtime_received=%d recovered=%d received=%d frames=%d expected=%d errors=%d unexpected_disconnects=%d read_failed=%d peak_conns=%d dial_attempted=%d dial_succeeded=%d dial_failed=%d ack_sent=%d resync_required=%d latency_samples=%d latency_p50=%s latency_p95=%s latency_p99=%s realtime_latency_samples=%d realtime_latency_p50=%s realtime_latency_p95=%s realtime_latency_p99=%s recovery_latency_samples=%d recovery_latency_p50=%s recovery_latency_p95=%s recovery_latency_p99=%s",
		duration.Round(time.Millisecond),
		stats.connected.Load(),
		stats.audience.Load(),
		stats.sent.Load(),
		stats.accepted.Load(),
		stats.rejected.Load(),
		stats.realtime.Load(),
		stats.recovered.Load(),
		stats.received.Load(),
		stats.frames.Load(),
		stats.expected.Load(),
		stats.errors.Load(),
		stats.unexpectedExit.Load(),
		stats.readFailed.Load(),
		stats.peakConnected.Load(),
		stats.dialAttempted.Load(),
		stats.dialSucceeded.Load(),
		stats.dialFailed.Load(),
		stats.ackSent.Load(),
		stats.resyncRequired.Load(),
		stats.latency.count.Load(),
		stats.latency.percentile(0.50),
		stats.latency.percentile(0.95),
		stats.latency.percentile(0.99),
		stats.realtimeLatency.count.Load(),
		stats.realtimeLatency.percentile(0.50),
		stats.realtimeLatency.percentile(0.95),
		stats.realtimeLatency.percentile(0.99),
		stats.recoveryLatency.count.Load(),
		stats.recoveryLatency.percentile(0.50),
		stats.recoveryLatency.percentile(0.95),
		stats.recoveryLatency.percentile(0.99),
	)
}
