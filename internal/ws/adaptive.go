package ws

import (
	"live-stream/internal/config"
	"live-stream/internal/model"
	"math"
	"time"
)

const policyRateWindow = time.Second

type adaptiveSettings struct {
	samplingEnabled    bool
	hotOnlineThreshold int
	maxFanoutPerSecond int64
	maxSampleStride    int
	hotPresencePeriod  time.Duration
	queueHighWatermark int
	slowStrikeLimit    uint32
	slowGrace          time.Duration
}

type roomAdaptiveState struct {
	windowStarted time.Time
	windowCount   uint64
	sampleCounter uint64
	sourceQPS     float64
	fanoutQPS     float64
	sampleStride  int
	hot           bool
	lastPresence  time.Time
	latestSeq     uint64
}

type deliveryPolicySnapshot struct {
	roomID       uint64
	hot          bool
	sourceQPS    float64
	fanoutQPS    float64
	sampleStride int
	latestSeq    uint64
}

func loadAdaptiveSettings() adaptiveSettings {
	settings := adaptiveSettings{
		samplingEnabled:    true,
		hotOnlineThreshold: 5000,
		maxFanoutPerSecond: 150000,
		maxSampleStride:    16,
		hotPresencePeriod:  time.Second,
		queueHighWatermark: clientQueueSize * 75 / 100,
		slowStrikeLimit:    100,
		slowGrace:          5 * time.Second,
	}
	if config.GlobalConfig == nil {
		return settings
	}
	cfg := config.GlobalConfig.Danmaku
	settings.samplingEnabled = cfg.HotRoom.AdaptiveSamplingEnabled
	if cfg.HotRoom.OnlineThreshold > 0 {
		settings.hotOnlineThreshold = cfg.HotRoom.OnlineThreshold
	}
	if cfg.HotRoom.MaxFanoutDeliveriesPerSecond > 0 {
		settings.maxFanoutPerSecond = cfg.HotRoom.MaxFanoutDeliveriesPerSecond
	}
	if cfg.HotRoom.MaxSampleStride > 0 {
		settings.maxSampleStride = cfg.HotRoom.MaxSampleStride
	}
	if cfg.HotRoom.PresenceIntervalMilliseconds > 0 {
		settings.hotPresencePeriod = time.Duration(cfg.HotRoom.PresenceIntervalMilliseconds) * time.Millisecond
	}
	if percent := cfg.SlowClient.QueueHighWatermarkPercent; percent > 0 && percent < 100 {
		settings.queueHighWatermark = clientQueueSize * percent / 100
	}
	if cfg.SlowClient.DisconnectStrikes > 0 {
		settings.slowStrikeLimit = uint32(cfg.SlowClient.DisconnectStrikes)
	}
	if cfg.SlowClient.DisconnectGraceMilliseconds > 0 {
		settings.slowGrace = time.Duration(cfg.SlowClient.DisconnectGraceMilliseconds) * time.Millisecond
	}
	return settings
}

func (h *Hub) planDanmaDelivery(message Message, now time.Time) (bool, *deliveryPolicySnapshot) {
	h.mu.Lock()
	room := h.rooms[message.RoomID]
	if room == nil {
		h.mu.Unlock()
		return false, nil
	}
	room.mu.Lock()
	h.mu.Unlock()
	defer room.mu.Unlock()

	state := &room.policy
	if state.windowStarted.IsZero() {
		state.windowStarted = now
		state.sampleStride = 1
	}
	state.windowCount++
	state.sampleCounter++
	if message.RoomSeq > state.latestSeq {
		state.latestSeq = message.RoomSeq
	}
	elapsed := now.Sub(state.windowStarted)
	policyChanged := false
	if elapsed >= policyRateWindow/2 {
		sourceQPS := float64(state.windowCount) / elapsed.Seconds()
		fanoutQPS := sourceQPS * float64(len(room.clients))
		stride := 1
		if h.adaptive.samplingEnabled && h.adaptive.maxFanoutPerSecond > 0 && fanoutQPS > float64(h.adaptive.maxFanoutPerSecond) {
			stride = int(math.Ceil(fanoutQPS / float64(h.adaptive.maxFanoutPerSecond)))
			if stride > h.adaptive.maxSampleStride {
				stride = h.adaptive.maxSampleStride
			}
		}
		hot := len(room.clients) >= h.adaptive.hotOnlineThreshold || stride > 1
		policyChanged = hot != state.hot || stride != state.sampleStride
		state.hot = hot
		state.sourceQPS = sourceQPS
		state.fanoutQPS = fanoutQPS
		state.sampleStride = stride
	}
	if elapsed >= policyRateWindow {
		state.windowStarted = now
		state.windowCount = 0
	}

	deliver := state.sampleStride <= 1 || state.sampleCounter%uint64(state.sampleStride) == 0
	if !policyChanged {
		return deliver, nil
	}
	return deliver, &deliveryPolicySnapshot{
		roomID:       message.RoomID,
		hot:          state.hot,
		sourceQPS:    state.sourceQPS,
		fanoutQPS:    state.fanoutQPS,
		sampleStride: state.sampleStride,
		latestSeq:    state.latestSeq,
	}
}

func (h *Hub) broadcastDeliveryPolicy(policy deliveryPolicySnapshot) {
	h.broadcastControlToRoom(deliveryPolicyMessage(policy))
}

func (h *Hub) enqueueDeliveryPolicy(client *Client, policy deliveryPolicySnapshot) {
	h.enqueueJSON(client, deliveryPolicyMessage(policy))
}

func deliveryPolicyMessage(policy deliveryPolicySnapshot) *model.Message {
	mode := "push"
	if policy.sampleStride > 1 {
		mode = "sampled"
	}
	return &model.Message{
		Type:    model.MsgTypeSystem,
		RoomID:  policy.roomID,
		RoomSeq: policy.latestSeq,
		Data: map[string]any{
			"sub_type":         "delivery_policy",
			"delivery_mode":    mode,
			"hot":              policy.hot,
			"source_qps":       policy.sourceQPS,
			"fanout_qps":       policy.fanoutQPS,
			"sample_stride":    policy.sampleStride,
			"poll_interval_ms": 1000,
		},
	}
}

func (h *Hub) roomIsHot(roomID uint64) bool {
	h.mu.Lock()
	room := h.rooms[roomID]
	if room == nil {
		h.mu.Unlock()
		return false
	}
	room.mu.RLock()
	h.mu.Unlock()
	hot := room.policy.hot || len(room.clients) >= h.adaptive.hotOnlineThreshold
	room.mu.RUnlock()
	return hot
}

func (h *Hub) presenceDue(roomID uint64, now time.Time) bool {
	h.mu.Lock()
	room := h.rooms[roomID]
	if room == nil {
		h.mu.Unlock()
		return false
	}
	room.mu.Lock()
	h.mu.Unlock()
	defer room.mu.Unlock()
	period := h.presencePeriod
	if room.policy.hot || len(room.clients) >= h.adaptive.hotOnlineThreshold {
		period = h.adaptive.hotPresencePeriod
	}
	if !room.policy.lastPresence.IsZero() && now.Sub(room.policy.lastPresence) < period {
		return false
	}
	room.policy.lastPresence = now
	return true
}
