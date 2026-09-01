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
		// 游客可看弹幕
		apiV1Group.GET("/ws", func(c *gin.Context) {
			ws.ServeWs(hub, c)
		})
		authRequierd := apiV1Group.Group("/auth")
		authRequierd.POST("/login", func(c *gin.Context) {
			handler.LoginHandler(c)
		})
		authRequierd.POST("/register", func(c *gin.Context) {
			handler.RegisterHandler(c)
		})
		authRequierd.Use(middleware.Auth)
		{
			// 用户相关
			authRequierd.GET("/me", func(c *gin.Context) {
				handler.GetMe(c)
			})
			authRequierd.POST("/logout", func(c *gin.Context) {
				handler.LogOut(c)
			})
		}

		rooms := apiV1Group.Group("/rooms")
		{
			rooms.GET("", func(c *gin.Context) {
				handler.ListRoom(c)
			})
			rooms.GET("/:room_id", func(c *gin.Context) {
				handler.GetRoomDetail(c)
			})
			// 弹幕历史
			rooms.GET("/:room_id/messages", func(c *gin.Context) {
				handler.GetDanmaHistory(c)
			})
			rooms.GET("/:room_id/messages/replay", func(c *gin.Context) {
				handler.ReplayDanmaHistory(c)
			})
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

}
