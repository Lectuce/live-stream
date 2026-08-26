package main

import (
	"context"
	"live-stream/internal/config"
	"live-stream/internal/dao"
	"live-stream/internal/kafka"
	"live-stream/internal/ws"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"
)

func main() {
	config.LoadConfig()

	hub := ws.NewHub()
	go hub.Run()
	dao.Initdb()
	dao.InitRedis()
	kafka.InitKafkaProducer()

	defer kafka.Close()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		if err := kafka.StartKafkaConsumer(ctx); err != nil {
			log.Printf("kafka consumer exit: %v", err)
		}
	}()

	go func() {
		for dm := range kafka.DanmuOutChan {
			// main同时持有hub和kafka通道，在这里转换消息，推给hub
			hub.Broadcast <- ws.Message{
				RoomID:  dm.RoomID,
				UserID:  dm.UserID,
				Content: dm.Content,
			}
		}
	}()
	r := gin.Default()

	r.GET("/ws", func(c *gin.Context) {
		ws.ServeWs(hub, c)
	})

	// 服务协程启动gin
	serverExit := make(chan struct{})
	go func() {
		defer close(serverExit)
		err := r.Run(":9000")
		if err != nil {
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

	// 等待gin服务退出
	<-serverExit
	log.Println("service exited")
}
