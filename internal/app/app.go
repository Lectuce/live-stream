// Package app owns application startup and shutdown.
package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"live-stream/internal/kafka"
	"live-stream/internal/ws"
)

const shutdownTimeout = 10 * time.Second

// Run blocks until cancellation or a server failure. Call once per process.
func Run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := initializeInfrastructure(ctx); err != nil {
		return err
	}
	defer kafka.Close()

	hub := ws.NewHub()
	defer hub.Stop()
	server, err := newHTTPServer(hub)
	if err != nil {
		return err
	}
	a := application{hub: hub, servers: []*http.Server{server, newDiagnosticsServer(hub)}}
	return a.run(ctx)
}

type application struct {
	hub     *ws.Hub
	servers []*http.Server
}

func (a *application) run(ctx context.Context) (result error) {
	listeners, err := listenServers(a.servers)
	if err != nil {
		return err
	}
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()

	// Keep workers available while in-flight HTTP requests drain.
	workerCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done, err := a.startWorkers(workerCtx)
	defer func() {
		stopCtx, stop := context.WithTimeout(context.Background(), shutdownTimeout)
		defer stop()
		result = errors.Join(result, shutdownServers(stopCtx, a.servers))
		cancel()
		a.hub.Stop()
		result = errors.Join(result, waitWorkers(stopCtx, done))
		log.Println("service exited")
	}()
	if err != nil {
		return err
	}

	serverErrors := make(chan error, len(a.servers))
	for i, server := range a.servers {
		listener := listeners[i]
		go func() {
			log.Printf("server listening on %s", listener.Addr())
			err := server.Serve(listener)
			if errors.Is(err, http.ErrServerClosed) {
				err = nil
			}
			serverErrors <- err
		}()
	}
	select {
	case <-ctx.Done():
		log.Println("shutdown requested")
		return nil
	case err := <-serverErrors:
		if err != nil {
			return fmt.Errorf("serve application: %w", err)
		}
		return nil
	}
}

func waitWorkers(ctx context.Context, done <-chan struct{}) error {
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for background workers: %w", ctx.Err())
	}
}
