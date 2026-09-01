package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

var broadcastDurationBounds = [...]time.Duration{
	100 * time.Microsecond,
	500 * time.Microsecond,
	time.Millisecond,
	5 * time.Millisecond,
	10 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	500 * time.Millisecond,
	time.Second,
	2 * time.Second,
	5 * time.Second,
}

type durationHistogram struct {
	count    atomic.Uint64
	sumNanos atomic.Uint64
	buckets  [len(broadcastDurationBounds) + 1]atomic.Uint64
}

func (h *durationHistogram) observe(duration time.Duration) {
	h.count.Add(1)
	h.sumNanos.Add(uint64(duration))
	for i, upperBound := range broadcastDurationBounds {
		if duration <= upperBound {
			h.buckets[i].Add(1)
			return
		}
	}
	h.buckets[len(h.buckets)-1].Add(1)
}

type hubMetrics struct {
	connections               atomic.Int64
	broadcastMessagesTotal    atomic.Uint64
	deliveryEnqueuedTotal     atomic.Uint64
	queueFullTotal            atomic.Uint64
	controlQueueFullTotal     atomic.Uint64
	resyncRequiredTotal       atomic.Uint64
	slowClientDisconnectTotal atomic.Uint64
	writeErrorsTotal          atomic.Uint64
	closedClientsTotal        atomic.Uint64
	ackTotal                  atomic.Uint64
	broadcastDuration         durationHistogram
	fanoutShardDuration       durationHistogram
	realtimeMessageAge        durationHistogram
}

type durationHistogramSnapshot struct {
	Count          uint64            `json:"count"`
	SumNanoseconds uint64            `json:"sum_nanoseconds"`
	Buckets        map[string]uint64 `json:"buckets"`
}

type MetricsSnapshot struct {
	Connections               int64                     `json:"ws_connections"`
	BroadcastMessagesTotal    uint64                    `json:"ws_broadcast_messages_total"`
	DeliveryEnqueuedTotal     uint64                    `json:"ws_delivery_enqueued_total"`
	QueueFullTotal            uint64                    `json:"ws_queue_full_total"`
	ControlQueueFullTotal     uint64                    `json:"ws_control_queue_full_total"`
	ResyncRequiredTotal       uint64                    `json:"ws_resync_required_total"`
	SlowClientDisconnectTotal uint64                    `json:"ws_slow_client_disconnect_total"`
	WriteErrorsTotal          uint64                    `json:"ws_write_errors_total"`
	ClosedClientsTotal        uint64                    `json:"ws_closed_clients_total"`
	AckTotal                  uint64                    `json:"ws_ack_total"`
	BroadcastDuration         durationHistogramSnapshot `json:"ws_broadcast_duration"`
	FanoutShardDuration       durationHistogramSnapshot `json:"ws_fanout_shard_duration"`
	RealtimeMessageAge        durationHistogramSnapshot `json:"ws_realtime_message_age"`
	ClientQueueDepth          int64                     `json:"ws_client_queue_depth"`
}

func (h *Hub) MetricsSnapshot() MetricsSnapshot {
	broadcastHistogram := snapshotDurationHistogram(&h.metrics.broadcastDuration)
	fanoutHistogram := snapshotDurationHistogram(&h.metrics.fanoutShardDuration)
	realtimeAgeHistogram := snapshotDurationHistogram(&h.metrics.realtimeMessageAge)

	return MetricsSnapshot{
		Connections:               h.metrics.connections.Load(),
		BroadcastMessagesTotal:    h.metrics.broadcastMessagesTotal.Load(),
		DeliveryEnqueuedTotal:     h.metrics.deliveryEnqueuedTotal.Load(),
		QueueFullTotal:            h.metrics.queueFullTotal.Load(),
		ControlQueueFullTotal:     h.metrics.controlQueueFullTotal.Load(),
		ResyncRequiredTotal:       h.metrics.resyncRequiredTotal.Load(),
		SlowClientDisconnectTotal: h.metrics.slowClientDisconnectTotal.Load(),
		WriteErrorsTotal:          h.metrics.writeErrorsTotal.Load(),
		ClosedClientsTotal:        h.metrics.closedClientsTotal.Load(),
		AckTotal:                  h.metrics.ackTotal.Load(),
		BroadcastDuration:         broadcastHistogram,
		FanoutShardDuration:       fanoutHistogram,
		RealtimeMessageAge:        realtimeAgeHistogram,
		ClientQueueDepth:          h.clientQueueDepth(),
	}
}

func snapshotDurationHistogram(histogram *durationHistogram) durationHistogramSnapshot {
	buckets := make(map[string]uint64, len(broadcastDurationBounds)+1)
	var cumulative uint64
	for i, upperBound := range broadcastDurationBounds {
		cumulative += histogram.buckets[i].Load()
		buckets[strconv.FormatInt(upperBound.Microseconds(), 10)+"us"] = cumulative
	}
	cumulative += histogram.buckets[len(broadcastDurationBounds)].Load()
	buckets["+Inf"] = cumulative
	return durationHistogramSnapshot{
		Count:          histogram.count.Load(),
		SumNanoseconds: histogram.sumNanos.Load(),
		Buckets:        buckets,
	}
}

func (h *Hub) clientQueueDepth() int64 {
	h.mu.Lock()
	rooms := make([]*Room, 0, len(h.rooms))
	for _, room := range h.rooms {
		rooms = append(rooms, room)
	}
	h.mu.Unlock()

	var depth int64
	for _, room := range rooms {
		room.mu.RLock()
		for client := range room.clients {
			depth += int64(len(client.send) + len(client.control) + len(client.state))
		}
		room.mu.RUnlock()
	}
	return depth
}

func (h *Hub) MetricsHandler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := json.NewEncoder(writer).Encode(h.MetricsSnapshot()); err != nil {
			log.Printf("encode websocket metrics failed: %v", err)
		}
	})
}

func (h *Hub) metricsWorker() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-ticker.C:
			metrics := h.MetricsSnapshot()
			log.Printf(
				"ws metrics connections=%d queue_full_total=%d control_queue_full_total=%d resync_required_total=%d slow_disconnect_total=%d write_errors_total=%d closed_clients_total=%d ack_total=%d client_queue_depth=%d",
				metrics.Connections,
				metrics.QueueFullTotal,
				metrics.ControlQueueFullTotal,
				metrics.ResyncRequiredTotal,
				metrics.SlowClientDisconnectTotal,
				metrics.WriteErrorsTotal,
				metrics.ClosedClientsTotal,
				metrics.AckTotal,
				metrics.ClientQueueDepth,
			)
		}
	}
}
