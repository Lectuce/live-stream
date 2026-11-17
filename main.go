package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"live-stream/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx); err != nil {
		log.Fatalf("application exited: %v", err)
	}
}
