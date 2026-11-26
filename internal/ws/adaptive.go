package ws

import (
	"live-stream/internal/config"
	"live-stream/internal/model"
	"math"
	"time"
)

const policyRateWindow = time.Second

type adaptiveSettings struct {
	enterWindows, exitWindows int
	holdPeriod                time.Duration
	samplingEnabled           bool
	hotOnlineThreshold        int
	maxFanoutPerSecond        int64
	maxSampleStride           int
	hotPresencePeriod         time.Duration
	queueHighWatermark        int
	slowStrikeLimit           uint32
	slowGrace                 time.Duration
}

type roomAdaptiveState struct {
	above, below  int
	changedAt     time.Time
	replayAfter   uint64
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
	replayAfter  uint64
}

func loadAdaptiveSettings() adaptiveSettings {
	settings := adaptiveSettings{
		enterWindows: 3, exitWindows: 5, holdPeriod: 10 * time.Second,
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
	if cfg.HotRoom.EnterWindows > 0 {
		settings.enterWindows = cfg.HotRoom.EnterWindows
	}
	if cfg.HotRoom.ExitWindows > 0 {
		settings.exitWindows = cfg.HotRoom.ExitWindows
	}
	if cfg.HotRoom.HoldSeconds > 0 {
		settings.holdPeriod = time.Duration(cfg.HotRoom.HoldSeconds) * time.Second
	}
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
	deliver := state.sampleStride <= 1 || state.sampleCounter%uint64(state.sampleStride) == 0
	if state.sampleStride <= 1 {
		state.replayAfter = message.RoomSeq
	}
	return deliver, nil
}

func (h *Hub) adaptiveWorker() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-h.ctx.Done():
			return
		case now := <-ticker.C:
			h.evaluatePolicies(now)
		}
	}
}

func (h *Hub) evaluatePolicies(now time.Time) {
	h.mu.Lock()
	rooms := make(map[uint64]*Room, len(h.rooms))
	for id, r := range h.rooms {
		rooms[id] = r
	}
	h.mu.Unlock()
	for id, r := range rooms {
		r.mu.Lock()
		s := &r.policy
		if s.windowStarted.IsZero() {
			s.windowStarted = now
			s.sampleStride = 1
			r.mu.Unlock()
			continue
		}
		elapsed := now.Sub(s.windowStarted).Seconds()
		if elapsed < 1 {
			r.mu.Unlock()
			continue
		}
		s.sourceQPS = float64(s.windowCount) / elapsed
		current := s.sourceQPS * float64(len(r.clients))
		s.fanoutQPS = .25*current + .75*s.fanoutQPS
		s.windowCount = 0
		s.windowStarted = now
		old := s.sampleStride
		budget := float64(h.adaptive.maxFanoutPerSecond)
		if s.fanoutQPS > budget {
			s.above++
		} else {
			s.above = 0
		}
		if s.fanoutQPS < budget*.7 {
			s.below++
		} else {
			s.below = 0
		}
		stride := int(math.Ceil(s.fanoutQPS / budget))
		if stride < 2 {
			stride = 2
		}
		if stride > h.adaptive.maxSampleStride {
			stride = h.adaptive.maxSampleStride
		}
		if !h.adaptive.samplingEnabled {
			s.sampleStride = 1
		} else if old <= 1 && s.above >= h.adaptive.enterWindows {
			s.sampleStride = stride
			s.changedAt = now
		} else if old > 1 && now.Sub(s.changedAt) >= h.adaptive.holdPeriod {
			if s.below >= h.adaptive.exitWindows {
				s.sampleStride = 1
			} else {
				s.sampleStride = stride
			}
			s.changedAt = now
		}
		s.hot = len(r.clients) >= h.adaptive.hotOnlineThreshold || s.sampleStride > 1
		policy := deliveryPolicySnapshot{roomID: id, hot: s.hot, sourceQPS: s.sourceQPS, fanoutQPS: s.fanoutQPS, sampleStride: s.sampleStride, latestSeq: s.latestSeq, replayAfter: s.replayAfter}
		r.mu.Unlock()
		if old != policy.sampleStride {
			h.broadcastDeliveryPolicy(policy)
		}
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
			"poll_interval_ms": 4000,
			"replay_after_seq": policy.replayAfter,
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
