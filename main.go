package main

import (
	"context"
	"errors"
	"live-stream/internal/config"
	"live-stream/internal/dao"
	"live-stream/internal/kafka"
	"live-stream/internal/rousters"
	"live-stream/internal/ws"
	"log"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

func main() {
	config.LoadConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := ws.NewHub()
	go hub.Run()
	diagnosticsServer := startDiagnosticsServer(hub)
	dao.Initdb()
	dao.InitRedis()
	if err := kafka.InitKafkaProducer(); err != nil {
		log.Fatalf("kafka producer init failed: %v", err)
	}

	defer kafka.Close()
	go func() {
		if err := ws.StartRedisSubscriber(ctx, hub); err != nil {
			log.Printf("redis subscriber exit: %v", err)
		}
	}()

	go func() {
		if err := kafka.StartKafkaConsumer(ctx); err != nil {
			log.Printf("kafka consumer exit: %v", err)
		}
	}()

	r := gin.Default()

	rousters.SetUpRousters(r, hub)

	// 服务协程启动gin
	appServer := &http.Server{
		Addr:              ":9000",
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}
	serverExit := make(chan struct{})
	go func() {
		defer close(serverExit)
		err := appServer.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("gin server run err: %v", err)
		}
	}()

	// 监听系统信号：Ctrl+C / kill
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	log.Println("receive shutdown signal, start exit")

	// 通知所有协程退出
	cancel()
	hub.Stop()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := appServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown application server: %v", err)
	}
	if err := diagnosticsServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown diagnostics server: %v", err)
	}

	// 等待gin服务退出
	<-serverExit
	log.Println("service exited")
}

func startDiagnosticsServer(hub *ws.Hub) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	mux.Handle("/debug/ws/metrics", hub.MetricsHandler())

	server := &http.Server{
		Addr:              "127.0.0.1:6060",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Printf("diagnostics server listening on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("diagnostics server exit: %v", err)
		}
	}()
	return server
}
