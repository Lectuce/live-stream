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
	connections            atomic.Int64
	broadcastMessagesTotal atomic.Uint64
	deliveryEnqueuedTotal  atomic.Uint64
	queueFullTotal         atomic.Uint64
	writeErrorsTotal       atomic.Uint64
	closedClientsTotal     atomic.Uint64
	broadcastDuration      durationHistogram
}

type durationHistogramSnapshot struct {
	Count          uint64            `json:"count"`
	SumNanoseconds uint64            `json:"sum_nanoseconds"`
	Buckets        map[string]uint64 `json:"buckets"`
}

type MetricsSnapshot struct {
	Connections            int64                     `json:"ws_connections"`
	BroadcastMessagesTotal uint64                    `json:"ws_broadcast_messages_total"`
	DeliveryEnqueuedTotal  uint64                    `json:"ws_delivery_enqueued_total"`
	QueueFullTotal         uint64                    `json:"ws_queue_full_total"`
	WriteErrorsTotal       uint64                    `json:"ws_write_errors_total"`
	ClosedClientsTotal     uint64                    `json:"ws_closed_clients_total"`
	BroadcastDuration      durationHistogramSnapshot `json:"ws_broadcast_duration"`
	ClientQueueDepth       int64                     `json:"ws_client_queue_depth"`
}

func (h *Hub) MetricsSnapshot() MetricsSnapshot {
	buckets := make(map[string]uint64, len(broadcastDurationBounds)+1)
	var cumulative uint64
	for i, upperBound := range broadcastDurationBounds {
		cumulative += h.metrics.broadcastDuration.buckets[i].Load()
		buckets[strconv.FormatInt(upperBound.Microseconds(), 10)+"us"] = cumulative
	}
	cumulative += h.metrics.broadcastDuration.buckets[len(broadcastDurationBounds)].Load()
	buckets["+Inf"] = cumulative

	return MetricsSnapshot{
		Connections:            h.metrics.connections.Load(),
		BroadcastMessagesTotal: h.metrics.broadcastMessagesTotal.Load(),
		DeliveryEnqueuedTotal:  h.metrics.deliveryEnqueuedTotal.Load(),
		QueueFullTotal:         h.metrics.queueFullTotal.Load(),
		WriteErrorsTotal:       h.metrics.writeErrorsTotal.Load(),
		ClosedClientsTotal:     h.metrics.closedClientsTotal.Load(),
		BroadcastDuration: durationHistogramSnapshot{
			Count:          h.metrics.broadcastDuration.count.Load(),
			SumNanoseconds: h.metrics.broadcastDuration.sumNanos.Load(),
			Buckets:        buckets,
		},
		ClientQueueDepth: h.clientQueueDepth(),
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
			depth += int64(len(client.send))
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
				"ws metrics connections=%d queue_full_total=%d write_errors_total=%d closed_clients_total=%d client_queue_depth=%d",
				metrics.Connections,
				metrics.QueueFullTotal,
				metrics.WriteErrorsTotal,
				metrics.ClosedClientsTotal,
				metrics.ClientQueueDepth,
			)
		}
	}
}
