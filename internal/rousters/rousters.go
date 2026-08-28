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
		apiV1Group.POST("/login", func(c *gin.Context) {
			handler.LoginHandler(c)
		})
		apiV1Group.POST("/register", func(c *gin.Context) {
			handler.RegisterHandler(c)
		})
		authRequierd := apiV1Group.Group("/")
		authRequierd.Use(middleware.Auth)
		{
			authRequierd.GET("/me", func(c *gin.Context) {
				handler.GetMe(c)
			})
			authRequierd.POST("/auth/logout", func(c *gin.Context) {
				handler.LogOut(c)
			})
			roomGroup := authRequierd.Group("/rooms")
			{
				roomGroup.POST("", func(c *gin.Context) {
					handler.CreateRoom(c)
				})
				roomGroup.GET("", func(c *gin.Context) {
					handler.ListRoom(c)
				})
				roomGroup.GET("/:id", func(c *gin.Context) {
					handler.GetRoomDetail(c)
				})
				roomGroup.PATCH("/:id", func(c *gin.Context) {
					handler.UpdateRoom(c)
				})
				roomGroup.POST("/:id/start", func(c *gin.Context) {
					handler.StartRoom(c)
				})
				roomGroup.POST("/:id/stop", func(c *gin.Context) {
					handler.StopRoom(c)
				})
			}
			authRequierd.GET("/ws", func(c *gin.Context) {
				ws.ServeWs(hub, c)
			})

		}

	}

}
