package main

import (
	"context"
	"live-stream/internal/config"
	"live-stream/internal/dao"
	"live-stream/internal/repo"
	"log"
	"os"
	"os/signal"
)

func main() {
	config.LoadConfig()
	dao.Initdb()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	var cursor uint64
	for {
		next, count, err := repo.BackfillVideoEvents(ctx, cursor)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("backfill scanned=%d cursor=%d", count, next)
		if count == 0 {
			return
		}
		cursor = next
	}
}
