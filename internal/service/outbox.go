package service

import (
	"context"
	"live-stream/internal/dao"
	"live-stream/internal/repo"
	"log"
	"time"
)

const outboxBatchSize = 100

// StartDanmaOutboxDispatcher publishes durable events in database order. A
// publish followed by a crash may cause a duplicate after restart; that is
// intentional at-least-once behavior and clients deduplicate by message_id.
func StartDanmaOutboxDispatcher(ctx context.Context) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		if err := dispatchDanmaOutbox(ctx); err != nil && ctx.Err() == nil {
			log.Printf("dispatch danma outbox failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func dispatchDanmaOutbox(ctx context.Context) error {
	items, err := repo.SelectPendingOutbox(ctx, outboxBatchSize)
	if err != nil {
		return err
	}
	for _, item := range items {
		if wait := time.Until(item.NextAttemptAt); wait > 0 {
			return nil
		}
		if err := dao.Publish(ctx, dao.DanmuChannel, string(item.Payload)); err != nil {
			delay := outboxRetryDelay(item.Attempts)
			if markErr := repo.MarkOutboxRetry(ctx, item.ID, time.Now().Add(delay)); markErr != nil {
				return markErr
			}
			return err
		}
		if err := repo.MarkOutboxPublished(ctx, item.ID); err != nil {
			return err
		}
	}
	return nil
}

func outboxRetryDelay(attempts uint) time.Duration {
	if attempts > 6 {
		attempts = 6
	}
	return 100 * time.Millisecond * time.Duration(1<<attempts)
}
