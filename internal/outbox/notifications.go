package outbox

import (
	"context"
	"live-stream/internal/repo"
	"log"
	"time"
)

func deliverNotifications(ctx context.Context) {
	for ctx.Err() == nil {
		count, err := repo.DeliverNotifications(ctx)
		if err != nil && ctx.Err() == nil {
			log.Printf("notification outbox: %v", err)
		}
		if err != nil || count < 100 {
			if !wait(ctx, time.Second) {
				return
			}
		}
	}
}
