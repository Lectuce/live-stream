package rousters

import (
	"live-stream/internal/handler"
	"live-stream/internal/middleware"
	"live-stream/internal/ws"

	"github.com/gin-gonic/gin"
)

func SetUpRousters(r *gin.Engine, hub *ws.Hub) {

	apiV1Group := r.Group("/api/v1")
	{
		// 公开接口（无需登录）
		apiV1Group.POST("/login", func(c *gin.Context) {
			handler.LoginHandler(c)
		})
		apiV1Group.POST("/register", func(c *gin.Context) {
			handler.RegisterHandler(c)
		})
		// 弹幕历史
		apiV1Group.POST("/room/:room_id/messages", func(c *gin.Context) {
			handler.GetDanmaHistory(c)
		})
		apiV1Group.GET("", func(c *gin.Context) {
			handler.ListRoom(c)
		})
		apiV1Group.GET("/:id", func(c *gin.Context) {
			handler.GetRoomDetail(c)
		})
		// 游客可看弹幕
		apiV1Group.GET("/ws", func(c *gin.Context) {
			ws.ServeWs(hub, c)
		})
		authRequierd := apiV1Group.Group("/")
		authRequierd.Use(middleware.Auth)
		{
			// 用户相关
			authRequierd.GET("/me", func(c *gin.Context) {
				handler.GetMe(c)
			})
			authRequierd.POST("/auth/logout", func(c *gin.Context) {
				handler.LogOut(c)
			})
		}

	}

	r.GET("/rooms", func(c *gin.Context) {
		handler.ListRoom(c)
	})
	r.GET("/rooms/:room_id", func(c *gin.Context) {
		handler.GetRoomDetail(c)
	})

	rooms := r.Group("/rooms")
	{
		rooms.Use(middleware.Auth)
		{
			rooms.POST("", func(c *gin.Context) {
				handler.CreateRoom(c)
			})
			rooms.PATCH("/:room_id", func(c *gin.Context) {
				handler.UpdateRoom(c)
			})
			rooms.POST("/:room_id/start", func(c *gin.Context) {
				handler.StartRoom(c)
			})
			rooms.POST("/:room_id/stop", func(c *gin.Context) {
				handler.StopRoom(c)
			})
		}
	}

}
