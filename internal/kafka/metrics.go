package kafka

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

var realtimeLatencyBounds = [...]time.Duration{
	time.Millisecond,
	5 * time.Millisecond,
	10 * time.Millisecond,
	25 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	250 * time.Millisecond,
	500 * time.Millisecond,
	time.Second,
	2 * time.Second,
	5 * time.Second,
}

type realtimeDurationHistogram struct {
	count    atomic.Uint64
	sumNanos atomic.Uint64
	maxNanos atomic.Uint64
	buckets  [len(realtimeLatencyBounds) + 1]atomic.Uint64
}

func (h *realtimeDurationHistogram) observe(value time.Duration) {
	if value < 0 {
		return
	}
	nanos := uint64(value)
	h.count.Add(1)
	h.sumNanos.Add(nanos)
	for {
		previous := h.maxNanos.Load()
		if nanos <= previous || h.maxNanos.CompareAndSwap(previous, nanos) {
			break
		}
	}
	for index, upperBound := range realtimeLatencyBounds {
		if value <= upperBound {
			h.buckets[index].Add(1)
			return
		}
	}
	h.buckets[len(h.buckets)-1].Add(1)
}

type realtimeDurationSnapshot struct {
	Count          uint64            `json:"count"`
	SumNanoseconds uint64            `json:"sum_nanoseconds"`
	MaxNanoseconds uint64            `json:"max_nanoseconds"`
	Buckets        map[string]uint64 `json:"buckets"`
}

type realtimeKafkaMetrics struct {
	messageAge       realtimeDurationHistogram
	redisPublish     realtimeDurationHistogram
	commitEnqueue    realtimeDurationHistogram
	fetchedTotal     atomic.Uint64
	publishedTotal   atomic.Uint64
	commitBatchTotal atomic.Uint64
}

var realtimeMetrics realtimeKafkaMetrics

type RealtimeMetricsSnapshot struct {
	FetchedTotal     uint64                   `json:"kafka_realtime_fetched_total"`
	PublishedTotal   uint64                   `json:"kafka_realtime_published_total"`
	CommitBatchTotal uint64                   `json:"kafka_realtime_commit_batch_total"`
	MessageAge       realtimeDurationSnapshot `json:"kafka_realtime_message_age"`
	RedisPublish     realtimeDurationSnapshot `json:"kafka_realtime_redis_publish_duration"`
	CommitEnqueue    realtimeDurationSnapshot `json:"kafka_realtime_commit_enqueue_duration"`
}

func RealtimeMetrics() RealtimeMetricsSnapshot {
	return RealtimeMetricsSnapshot{
		FetchedTotal:     realtimeMetrics.fetchedTotal.Load(),
		PublishedTotal:   realtimeMetrics.publishedTotal.Load(),
		CommitBatchTotal: realtimeMetrics.commitBatchTotal.Load(),
		MessageAge:       snapshotRealtimeDuration(&realtimeMetrics.messageAge),
		RedisPublish:     snapshotRealtimeDuration(&realtimeMetrics.redisPublish),
		CommitEnqueue:    snapshotRealtimeDuration(&realtimeMetrics.commitEnqueue),
	}
}

func snapshotRealtimeDuration(histogram *realtimeDurationHistogram) realtimeDurationSnapshot {
	buckets := make(map[string]uint64, len(realtimeLatencyBounds)+1)
	var cumulative uint64
	for index, upperBound := range realtimeLatencyBounds {
		cumulative += histogram.buckets[index].Load()
		buckets[strconv.FormatInt(upperBound.Microseconds(), 10)+"us"] = cumulative
	}
	cumulative += histogram.buckets[len(histogram.buckets)-1].Load()
	buckets["+Inf"] = cumulative
	return realtimeDurationSnapshot{
		Count:          histogram.count.Load(),
		SumNanoseconds: histogram.sumNanos.Load(),
		MaxNanoseconds: histogram.maxNanos.Load(),
		Buckets:        buckets,
	}
}

func RealtimeMetricsHandler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(writer).Encode(RealtimeMetrics())
	})
}
