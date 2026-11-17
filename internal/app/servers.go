package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/gin-gonic/gin"
	"live-stream/internal/kafka"
	"live-stream/internal/rousters"
	"live-stream/internal/ws"
)

func newHTTPServer(hub *ws.Hub) (*http.Server, error) {
	router := gin.Default()
	if err := router.SetTrustedProxies([]string{"127.0.0.1", "::1"}); err != nil {
		return nil, fmt.Errorf("configure trusted proxies: %w", err)
	}
	rousters.SetUpRousters(router, hub)
	return &http.Server{Addr: ":9000", Handler: router, ReadHeaderTimeout: 5 * time.Second}, nil
}

func newDiagnosticsServer(hub *ws.Hub) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	mux.Handle("/debug/ws/metrics", hub.MetricsHandler())
	mux.Handle("/debug/kafka/realtime", kafka.RealtimeMetricsHandler())
	return &http.Server{Addr: "127.0.0.1:6060", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
}

// Bind all ports before starting workers; roll back earlier listeners on failure.
func listenServers(servers []*http.Server) ([]net.Listener, error) {
	listeners := make([]net.Listener, 0, len(servers))
	for _, server := range servers {
		listener, err := net.Listen("tcp", server.Addr)
		if err != nil {
			for _, opened := range listeners {
				_ = opened.Close()
			}
			return nil, fmt.Errorf("listen on %s: %w", server.Addr, err)
		}
		listeners = append(listeners, listener)
	}
	return listeners, nil
}

func shutdownServers(ctx context.Context, servers []*http.Server) error {
	results := make(chan error, len(servers))
	for _, server := range servers {
		go func() {
			err := server.Shutdown(ctx)
			if err != nil {
				_ = server.Close()
				err = fmt.Errorf("shutdown %s: %w", server.Addr, err)
			}
			results <- err
		}()
	}
	var result error
	for range servers {
		result = errors.Join(result, <-results)
	}
	return result
}
