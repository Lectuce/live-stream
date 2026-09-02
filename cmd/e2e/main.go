package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type testConfig struct {
	wsURL                   string
	origin                  string
	roomID                  uint64
	sid                     string
	userID                  uint64
	userName                string
	timeout                 time.Duration
	expectedHandshakeStatus int
	likeRateLimit           int
}

type wireMessage struct {
	Type      string         `json:"type"`
	Code      int            `json:"code"`
	MessageID string         `json:"message_id"`
	RoomID    uint64         `json:"room_id"`
	RoomSeq   uint64         `json:"room_seq"`
	UserID    uint64         `json:"user_id"`
	UserName  string         `json:"user_name"`
	Content   string         `json:"content"`
	Data      map[string]any `json:"data"`
}

type result struct {
	MessageIDs           []string `json:"message_ids"`
	RoomSequences        []uint64 `json:"room_sequences"`
	DuplicatedDanmaID    string   `json:"duplicated_danma_id"`
	LikeMessageID        string   `json:"like_message_id"`
	LikeCount            int64    `json:"like_count"`
	DuplicateLikeApplied bool     `json:"duplicate_like_applied"`
	LikeRateLimited      bool     `json:"like_rate_limited"`
	GeneratedIDValid     bool     `json:"generated_id_valid"`
	GuestReadOK          bool     `json:"guest_read_ok"`
	GuestSendBlocked     bool     `json:"guest_send_blocked"`
	SpoofingBlocked      bool     `json:"spoofing_blocked"`
	OrderingOK           bool     `json:"ordering_ok"`
	AckSent              bool     `json:"ack_sent"`
	OriginBlocked        bool     `json:"origin_blocked"`
}

func main() {
	var cfg testConfig
	flag.StringVar(&cfg.wsURL, "ws-url", "ws://127.0.0.1:9000/api/v1/ws", "WebSocket endpoint")
	flag.StringVar(&cfg.origin, "origin", "http://localhost:5173", "allowed Origin header")
	flag.Uint64Var(&cfg.roomID, "room", 0, "live room ID")
	flag.StringVar(&cfg.sid, "sid", "", "authenticated sid cookie")
	flag.Uint64Var(&cfg.userID, "user", 0, "expected authenticated user ID")
	flag.StringVar(&cfg.userName, "username", "", "expected authenticated username")
	flag.DurationVar(&cfg.timeout, "timeout", 15*time.Second, "timeout for each protocol phase")
	flag.IntVar(&cfg.expectedHandshakeStatus, "expect-handshake-status", 0, "only assert that the handshake fails with this HTTP status")
	flag.IntVar(&cfg.likeRateLimit, "like-rate-limit", 10, "expected likes per user/room/second; 0 skips the check")
	flag.Parse()

	if cfg.roomID == 0 {
		log.Fatal("-room is required")
	}
	if cfg.expectedHandshakeStatus != 0 {
		conn, response, err := dial(cfg, cfg.origin, cfg.sid)
		if conn != nil {
			conn.Close()
			log.Fatalf("handshake unexpectedly succeeded; wanted HTTP %d", cfg.expectedHandshakeStatus)
		}
		status := 0
		if response != nil {
			status = response.StatusCode
		}
		if err == nil || status != cfg.expectedHandshakeStatus {
			log.Fatalf("handshake status=%d error=%v, want %d", status, err, cfg.expectedHandshakeStatus)
		}
		fmt.Printf("[E2E_HANDSHAKE] status=%d\n", status)
		return
	}
	if cfg.sid == "" || cfg.userID == 0 || cfg.userName == "" {
		log.Fatal("-sid, -user, and -username are required for the full protocol test")
	}

	r, err := run(cfg)
	if err != nil {
		log.Fatalf("E2E protocol test failed: %v", err)
	}
	payload, err := json.Marshal(r)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("[E2E_JSON] %s\n", payload)
}

func run(cfg testConfig) (result, error) {
	r := result{}
	if err := expectOriginRejected(cfg); err != nil {
		return r, err
	}
	r.OriginBlocked = true

	guest, _, err := dial(cfg, cfg.origin, "")
	if err != nil {
		return r, fmt.Errorf("guest connection: %w", err)
	}
	r.GuestReadOK = true
	if err := guest.WriteJSON(map[string]any{"type": "danma", "content": "guest-must-not-send"}); err != nil {
		guest.Close()
		return r, fmt.Errorf("guest write: %w", err)
	}
	guestError, err := readUntil(guest, cfg.timeout, func(m wireMessage) bool {
		return m.Type == "error" && m.Code == http.StatusUnauthorized
	})
	guest.Close()
	if err != nil {
		return r, fmt.Errorf("guest send was not rejected: %w", err)
	}
	if guestError.Code != http.StatusUnauthorized {
		return r, fmt.Errorf("guest rejection code=%d, want 401", guestError.Code)
	}
	r.GuestSendBlocked = true

	receiverA, _, err := dial(cfg, cfg.origin, "")
	if err != nil {
		return r, fmt.Errorf("receiver A: %w", err)
	}
	defer receiverA.Close()
	receiverB, _, err := dial(cfg, cfg.origin, "")
	if err != nil {
		return r, fmt.Errorf("receiver B: %w", err)
	}
	defer receiverB.Close()
	sender, _, err := dial(cfg, cfg.origin, cfg.sid)
	if err != nil {
		return r, fmt.Errorf("authenticated sender: %w", err)
	}
	defer sender.Close()

	if err := sender.WriteJSON(map[string]any{
		"type":       "danma",
		"message_id": "not-a-uuid",
		"content":    "invalid-message-id",
	}); err != nil {
		return r, err
	}
	if _, err := readUntil(sender, cfg.timeout, func(m wireMessage) bool {
		return m.Type == "error" && m.Code == http.StatusBadRequest
	}); err != nil {
		return r, fmt.Errorf("invalid message_id validation: %w", err)
	}

	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)
	contents := []string{
		"e2e-generated-" + stamp,
		"e2e-explicit-a-" + stamp,
		"e2e-explicit-b-" + stamp,
	}
	messageIDs := []string{"", uuid.NewString(), uuid.NewString()}
	for i := range contents {
		outbound := map[string]any{
			"type":      "danma",
			"room_id":   cfg.roomID + 999,
			"user_id":   cfg.userID + 999,
			"user_name": "spoofed-user",
			"content":   contents[i],
		}
		if messageIDs[i] != "" {
			outbound["message_id"] = messageIDs[i]
		}
		if err := sender.WriteJSON(outbound); err != nil {
			return r, fmt.Errorf("send message %d: %w", i, err)
		}
		accepted, err := readUntil(sender, cfg.timeout, func(m wireMessage) bool {
			if m.Type != "accepted" {
				return false
			}
			return messageIDs[i] == "" || m.MessageID == messageIDs[i]
		})
		if err != nil {
			return r, fmt.Errorf("wait accepted %d: %w", i, err)
		}
		if messageIDs[i] == "" {
			if _, err := uuid.Parse(accepted.MessageID); err != nil {
				return r, fmt.Errorf("server-generated message_id is not UUID: %q", accepted.MessageID)
			}
			messageIDs[i] = accepted.MessageID
			r.GeneratedIDValid = true
		}
	}

	sequencesA, err := collectMessages(receiverA, cfg, messageIDs, contents)
	if err != nil {
		return r, fmt.Errorf("receiver A: %w", err)
	}
	sequencesB, err := collectMessages(receiverB, cfg, messageIDs, contents)
	if err != nil {
		return r, fmt.Errorf("receiver B: %w", err)
	}
	if !equalUint64(sequencesA, sequencesB) {
		return r, fmt.Errorf("receivers observed different room order: A=%v B=%v", sequencesA, sequencesB)
	}
	if !strictlyIncreasing(sequencesA) {
		return r, fmt.Errorf("room_seq is not strictly increasing: %v", sequencesA)
	}
	r.MessageIDs = messageIDs
	r.RoomSequences = sequencesA
	r.SpoofingBlocked = true
	r.OrderingOK = true

	// Retry one logical danmaku with the same UUID. The persistence test uses
	// this ID to verify that MySQL stores exactly one row.
	if err := sender.WriteJSON(map[string]any{
		"type":       "danma",
		"message_id": messageIDs[1],
		"content":    contents[1],
	}); err != nil {
		return r, fmt.Errorf("retry duplicate danmaku: %w", err)
	}
	if _, err := readUntil(sender, cfg.timeout, func(m wireMessage) bool {
		return m.Type == "accepted" && m.MessageID == messageIDs[1]
	}); err != nil {
		return r, fmt.Errorf("wait duplicate danmaku accepted: %w", err)
	}
	r.DuplicatedDanmaID = messageIDs[1]

	if err := receiverA.WriteJSON(map[string]any{
		"type":    "ack",
		"room_id": cfg.roomID,
		"ack_seq": sequencesA[len(sequencesA)-1],
	}); err != nil {
		return r, fmt.Errorf("send ACK: %w", err)
	}
	r.AckSent = true

	likeID := uuid.NewString()
	if err := sender.WriteJSON(map[string]any{
		"type":       "like",
		"message_id": likeID,
		"room_id":    cfg.roomID + 999,
		"user_id":    cfg.userID + 999,
	}); err != nil {
		return r, fmt.Errorf("send like: %w", err)
	}
	if _, err := readUntil(sender, cfg.timeout, func(m wireMessage) bool {
		return m.Type == "accepted" && m.MessageID == likeID
	}); err != nil {
		return r, fmt.Errorf("wait like accepted: %w", err)
	}
	state, err := readUntil(receiverA, cfg.timeout, func(m wireMessage) bool {
		if m.Type != "system" || m.Data == nil || m.Data["sub_type"] != "room_state" {
			return false
		}
		count, ok := numberAsInt64(m.Data["like_count"])
		return ok && count > 0
	})
	if err != nil {
		return r, fmt.Errorf("wait like state: %w", err)
	}
	r.LikeMessageID = likeID
	r.LikeCount, _ = numberAsInt64(state.Data["like_count"])

	// Re-submit the exact same logical like. The count must remain unchanged;
	// duplicate_like_applied=true exposes a durable idempotency regression.
	if err := sender.WriteJSON(map[string]any{
		"type":       "like",
		"message_id": likeID,
		"room_id":    cfg.roomID,
	}); err != nil {
		return r, fmt.Errorf("send duplicate like: %w", err)
	}
	if _, err := readUntil(sender, cfg.timeout, func(m wireMessage) bool {
		return m.Type == "accepted" && m.MessageID == likeID
	}); err != nil {
		return r, fmt.Errorf("wait duplicate like accepted: %w", err)
	}
	duplicateDeadline := cfg.timeout
	if duplicateDeadline > 3*time.Second {
		duplicateDeadline = 3 * time.Second
	}
	duplicateState, duplicateErr := readUntil(receiverA, duplicateDeadline, func(m wireMessage) bool {
		if m.Type != "system" || m.Data == nil || m.Data["sub_type"] != "room_state" {
			return false
		}
		count, ok := numberAsInt64(m.Data["like_count"])
		return ok && count > r.LikeCount
	})
	if duplicateErr == nil {
		count, _ := numberAsInt64(duplicateState.Data["like_count"])
		r.DuplicateLikeApplied = count > r.LikeCount
		r.LikeCount = count
	}
	if cfg.likeRateLimit > 0 {
		now := time.Now()
		nextSecond := time.Unix(now.Unix()+1, 0).Add(20 * time.Millisecond)
		time.Sleep(time.Until(nextSecond))
		for i := 0; i < cfg.likeRateLimit+3; i++ {
			burstID := uuid.NewString()
			if err := sender.WriteJSON(map[string]any{
				"type":       "like",
				"message_id": burstID,
			}); err != nil {
				return r, fmt.Errorf("send rate-limit like %d: %w", i, err)
			}
			response, err := readUntil(sender, cfg.timeout, func(m wireMessage) bool {
				return m.MessageID == burstID &&
					(m.Type == "accepted" || (m.Type == "error" && m.Code == http.StatusTooManyRequests))
			})
			if err != nil {
				return r, fmt.Errorf("wait rate-limit response %d: %w", i, err)
			}
			if response.Type == "error" && response.Code == http.StatusTooManyRequests {
				r.LikeRateLimited = true
				break
			}
		}
		if !r.LikeRateLimited {
			return r, fmt.Errorf("like rate limit did not reject a burst above %d/s", cfg.likeRateLimit)
		}
	}
	return r, nil
}

func expectOriginRejected(cfg testConfig) error {
	conn, response, err := dial(cfg, "http://e2e-origin-not-allowed.invalid", "")
	if conn != nil {
		conn.Close()
		return fmt.Errorf("invalid Origin unexpectedly upgraded")
	}
	if err == nil {
		return fmt.Errorf("invalid Origin unexpectedly succeeded")
	}
	if response == nil || response.StatusCode != http.StatusForbidden {
		status := 0
		if response != nil {
			status = response.StatusCode
		}
		return fmt.Errorf("invalid Origin status=%d, want 403", status)
	}
	return nil
}

func dial(cfg testConfig, origin, sid string) (*websocket.Conn, *http.Response, error) {
	u, err := url.Parse(cfg.wsURL)
	if err != nil {
		return nil, nil, err
	}
	query := u.Query()
	query.Set("room_id", strconv.FormatUint(cfg.roomID, 10))
	u.RawQuery = query.Encode()
	header := http.Header{}
	header.Set("Origin", origin)
	if sid != "" {
		header.Set("Cookie", "sid="+sid)
	}
	dialer := websocket.Dialer{HandshakeTimeout: cfg.timeout}
	return dialer.Dial(u.String(), header)
}

func readUntil(conn *websocket.Conn, timeout time.Duration, predicate func(wireMessage) bool) (wireMessage, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := conn.SetReadDeadline(deadline); err != nil {
			return wireMessage{}, err
		}
		_, payload, err := conn.ReadMessage()
		if err != nil {
			return wireMessage{}, err
		}
		var message wireMessage
		if err := json.Unmarshal(payload, &message); err != nil {
			continue
		}
		if predicate(message) {
			return message, nil
		}
	}
	return wireMessage{}, fmt.Errorf("timeout")
}

func collectMessages(conn *websocket.Conn, cfg testConfig, ids, contents []string) ([]uint64, error) {
	expected := make(map[string]int, len(ids))
	for i, id := range ids {
		expected[id] = i
	}
	observed := make([]wireMessage, 0, len(ids))
	deadline := time.Now().Add(cfg.timeout)
	for len(observed) < len(ids) {
		message, err := readUntil(conn, time.Until(deadline), func(m wireMessage) bool {
			_, ok := expected[m.MessageID]
			return m.Type == "danma" && ok
		})
		if err != nil {
			return nil, err
		}
		index := expected[message.MessageID]
		if message.RoomID != cfg.roomID || message.UserID != cfg.userID || message.UserName != cfg.userName {
			return nil, fmt.Errorf("identity spoofing was not overwritten: %+v", message)
		}
		if message.Content != contents[index] {
			return nil, fmt.Errorf("content mismatch for %s", message.MessageID)
		}
		observed = append(observed, message)
		delete(expected, message.MessageID)
	}
	sequences := make([]uint64, 0, len(observed))
	for _, message := range observed {
		sequences = append(sequences, message.RoomSeq)
	}
	return sequences, nil
}

func strictlyIncreasing(values []uint64) bool {
	for i := 1; i < len(values); i++ {
		if values[i] <= values[i-1] {
			return false
		}
	}
	return true
}

func equalUint64(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func numberAsInt64(value any) (int64, bool) {
	switch number := value.(type) {
	case float64:
		return int64(number), true
	case json.Number:
		value, err := number.Int64()
		return value, err == nil
	case int64:
		return number, true
	case string:
		value, err := strconv.ParseInt(strings.TrimSpace(number), 10, 64)
		return value, err == nil
	default:
		return 0, false
	}
}

func init() {
	log.SetOutput(os.Stderr)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
}
