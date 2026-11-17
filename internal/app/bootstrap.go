package app

import (
	"context"
	"fmt"
	"log"

	"live-stream/internal/config"
	"live-stream/internal/dao"
	"live-stream/internal/kafka"
)

// Keep the existing DAO initialization and automatic migrations in one place.
func initializeInfrastructure(ctx context.Context) error {
	config.LoadConfig()
	dao.Initdb()
	dao.InitRedis()
	count, err := dao.ReconcileRoomLikeCounts(ctx, true)
	if err != nil {
		return fmt.Errorf("restore room like counts: %w", err)
	}
	log.Printf("room like counts restored, rooms=%d", count)
	if err := kafka.InitKafkaProducer(); err != nil {
		return fmt.Errorf("initialize kafka producer: %w", err)
	}
	return nil
}
