package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	appconfig "live-stream/internal/config"
	"live-stream/internal/model"

	"github.com/segmentio/kafka-go"
)

type metrics struct {
	queued      atomic.Int64
	delivered   atomic.Int64
	failed      atomic.Int64
	writeErrors atomic.Int64
}

func main() {
	cfg := appconfig.LoadConfig()
	defaultBrokers := strings.Join(cfg.Kafka.Brokers, ",")

	brokersFlag := flag.String("brokers", defaultBrokers, "comma-separated Kafka broker addresses")
	topic := flag.String("topic", cfg.Kafka.DanmuTopic, "Kafka topic")
	total := flag.Int64("n", 1_000_000, "total number of messages")
	workers := flag.Int("w", 100, "number of concurrent writers")
	roomID := flag.Uint64("room", 1, "room ID used as the Kafka key")
	userID := flag.Uint64("user", 1, "mock user ID")
	async := flag.Bool("async", true, "use asynchronous kafka-go writes")
	batchSize := flag.Int("batch", 1000, "maximum messages per Kafka batch")
	batchTimeout := flag.Duration("batch-timeout", 10*time.Millisecond, "maximum time to wait before flushing a batch")
	pace := flag.Duration("pace", 0, "optional delay after each message per worker")
	reportInterval := flag.Duration("report", time.Second, "metrics reporting interval")
	flag.Parse()

	if *total <= 0 {
		log.Fatal("-n must be greater than 0")
	}
	if *workers <= 0 {
		log.Fatal("-w must be greater than 0")
	}
	if *roomID == 0 {
		log.Fatal("-room must be greater than 0")
	}
	if *batchSize <= 0 {
		log.Fatal("-batch must be greater than 0")
	}
	if *batchTimeout <= 0 {
		log.Fatal("-batch-timeout must be greater than 0")
	}
	if *pace < 0 {
		log.Fatal("-pace cannot be negative")
	}
	if *reportInterval <= 0 {
		log.Fatal("-report must be greater than 0")
	}

	brokers := splitBrokers(*brokersFlag)
	if len(brokers) == 0 {
		log.Fatal("-brokers must contain at least one broker")
	}
	if strings.TrimSpace(*topic) == "" {
		log.Fatal("-topic cannot be empty")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var stats metrics
	writer := &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        *topic,
		Balancer:     &kafka.Hash{},
		Async:        *async,
		RequiredAcks: kafka.RequireAll,
		BatchSize:    *batchSize,
		BatchTimeout: *batchTimeout,
		Completion: func(messages []kafka.Message, err error) {
			count := int64(len(messages))
			if err != nil {
				stats.failed.Add(count)
				return
			}
			stats.delivered.Add(count)
		},
	}

	log.Printf(
		"[KAFKA BENCHMARK] starting brokers=%v topic=%s total=%d workers=%d async=%t batch=%d",
		brokers,
		*topic,
		*total,
		*workers,
		*async,
		*batchSize,
	)

	startedAt := time.Now()
	done := make(chan struct{})
	go monitor(ctx, done, &stats, startedAt, *reportInterval)

	startGate := make(chan struct{})
	var wg sync.WaitGroup
	baseCount := *total / int64(*workers)
	remainder := *total % int64(*workers)
	runID := startedAt.UnixNano()
	key := []byte(strconv.FormatUint(*roomID, 10))

	for workerID := 0; workerID < *workers; workerID++ {
		count := baseCount
		if int64(workerID) < remainder {
			count++
		}
		wg.Add(1)
		go func(id int, messageCount int64) {
			defer wg.Done()
			<-startGate
			for sequence := int64(0); sequence < messageCount; sequence++ {
				if ctx.Err() != nil {
					return
				}

				message := model.Message{
					MessageID: fmt.Sprintf("bench-%d-%d-%d", runID, id, sequence),
					RoomID:    *roomID,
					UserID:    *userID,
					UserName:  fmt.Sprintf("bench-worker-%d", id),
					Content:   fmt.Sprintf("kafka-benchmark-%d-%d", id, sequence),
					Type:      model.MsgTypeDanma,
				}
				payload, err := json.Marshal(message)
				if err != nil {
					stats.writeErrors.Add(1)
					continue
				}

				err = writer.WriteMessages(ctx, kafka.Message{Key: key, Value: payload})
				if err != nil {
					stats.writeErrors.Add(1)
					continue
				}
				stats.queued.Add(1)

				if *pace > 0 {
					select {
					case <-ctx.Done():
						return
					case <-time.After(*pace):
					}
				}
			}
		}(workerID, count)
	}

	close(startGate)
	wg.Wait()
	enqueueDuration := time.Since(startedAt)
	closeErr := writer.Close()
	totalDuration := time.Since(startedAt)
	close(done)

	if closeErr != nil {
		log.Printf("[KAFKA BENCHMARK] writer close failed: %v", closeErr)
	}

	queued := stats.queued.Load()
	delivered := stats.delivered.Load()
	log.Printf(
		"[FINAL] queued=%d delivered=%d failed=%d write_errors=%d enqueue_duration=%s total_with_flush=%s enqueue_qps=%.2f delivered_qps=%.2f",
		queued,
		delivered,
		stats.failed.Load(),
		stats.writeErrors.Load(),
		enqueueDuration.Round(time.Millisecond),
		totalDuration.Round(time.Millisecond),
		rate(queued, enqueueDuration),
		rate(delivered, totalDuration),
	)
}

func splitBrokers(raw string) []string {
	parts := strings.Split(raw, ",")
	brokers := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			brokers = append(brokers, part)
		}
	}
	return brokers
}

func monitor(ctx context.Context, done <-chan struct{}, stats *metrics, startedAt time.Time, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastQueued int64
	var lastDelivered int64
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			queued := stats.queued.Load()
			delivered := stats.delivered.Load()
			log.Printf(
				"[STATS] elapsed=%s queued/s=%d delivered/s=%d queued=%d delivered=%d failed=%d write_errors=%d",
				time.Since(startedAt).Round(time.Second),
				queued-lastQueued,
				delivered-lastDelivered,
				queued,
				delivered,
				stats.failed.Load(),
				stats.writeErrors.Load(),
			)
			lastQueued = queued
			lastDelivered = delivered
		}
	}
}

func rate(count int64, duration time.Duration) float64 {
	if duration <= 0 {
		return 0
	}
	return float64(count) / duration.Seconds()
}
