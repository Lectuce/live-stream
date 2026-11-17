package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"live-stream/internal/config"
	"live-stream/internal/dao"
	"live-stream/internal/hotrank"
	"live-stream/internal/kafka"
	"live-stream/internal/outbox"
	"live-stream/internal/service"
	"live-stream/internal/ws"
)

func (a *application) startWorkers(ctx context.Context) (<-chan struct{}, error) {
	mediaDone, err := service.StartLiveMedia(ctx)
	if err != nil {
		if os.Getenv("LIVE_MEDIA_SECRET") != "" {
			return mediaDone, fmt.Errorf("start configured live media: %w", err)
		}
		log.Printf("live media disabled: %v", err)
	}

	var workers sync.WaitGroup
	start := func(name string, run func() error) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := run(); err != nil && ctx.Err() == nil {
				log.Printf("%s exited: %v", name, err)
			}
		}()
	}
	start("hub", func() error { a.hub.Run(); return nil })
	start("like reconciler", func() error { runLikeReconciler(ctx); return nil })
	if config.GlobalConfig.Danmaku.HotRanking.Enabled {
		start("hot ranking", func() error { hotrank.New(a.hub).Run(ctx); return nil })
	}
	start("redis subscriber", func() error { return ws.StartRedisSubscriber(ctx, a.hub) })
	start("kafka persistence consumer", func() error { return kafka.StartKafkaConsumer(ctx) })
	start("kafka realtime consumer", func() error { return kafka.StartRealtimeConsumer(ctx) })
	contentDone := outbox.Start(ctx, config.GlobalConfig.Kafka.Brokers)
	done := make(chan struct{})
	go func() {
		defer close(done)
		workers.Wait()
		<-mediaDone
		<-contentDone
	}()
	return done, nil
}

func runLikeReconciler(ctx context.Context) {
	interval := 30 * time.Second
	if seconds := config.GlobalConfig.Danmaku.Like.ReconcileIntervalSeconds; seconds > 0 {
		interval = time.Duration(seconds) * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := dao.ReconcileRoomLikeCounts(ctx, false); err != nil && ctx.Err() == nil {
				log.Printf("reconcile room like counts: %v", err)
			}
		}
	}
}
