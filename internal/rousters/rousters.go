package rousters

import (
	"live-stream/internal/handler"
	"live-stream/internal/middleware"
	"live-stream/internal/ws"

	"github.com/gin-gonic/gin"
)

func SetUpRousters(r *gin.Engine, hub *ws.Hub) {
	r.Use(middleware.CORS())

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
			rooms.GET("/mine", middleware.Auth, func(c *gin.Context) {
				handler.GetMyRoom(c)
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

		feeds := apiV1Group.Group("/feeds")
		{
			feeds.GET("/live/hot", handler.GetHotLiveFeed)
			feeds.GET("/following", middleware.Auth, handler.GetFollowingFeed)

		}

		users := apiV1Group.Group("/users")
		users.GET("/:user_id", handler.GetPublicUser)
		users.GET("/:user_id/videos", handler.ListPublicAuthorVideos)
		users.GET("/:user_id/posts", handler.ListAuthorPosts)
		users.GET("/:user_id/follow", middleware.Auth, handler.UserFollow)
		users.PUT("/:user_id/follow", middleware.Auth, middleware.CommunityLimit("follow"), handler.UserFollow)
		users.DELETE("/:user_id/follow", middleware.Auth, middleware.CommunityLimit("follow"), handler.UserFollow)

		posts := apiV1Group.Group("/posts")
		posts.GET("/images/:image_id", handler.ServePostImage)
		posts.HEAD("/images/:image_id", handler.ServePostImage)
		posts.POST("/images", middleware.Auth, handler.UploadPostImage)
		posts.GET("/mine", middleware.Auth, handler.ListMyPosts)
		posts.GET("/mine/:post_id", middleware.Auth, handler.GetMyPost)
		posts.GET("/:post_id/like", handler.PostLike)
		posts.PUT("/:post_id/like", middleware.Auth, middleware.CommunityLimit("interaction"), handler.PostLike)
		posts.DELETE("/:post_id/like", middleware.Auth, middleware.CommunityLimit("interaction"), handler.PostLike)
		posts.GET("/:post_id", handler.GetPost)
		posts.GET("/:post_id/comments", handler.ListPostComments)
		posts.GET("/:post_id/comments/:comment_id/replies", handler.ListPostCommentReplies)
		posts.POST("/:post_id/comments", middleware.Auth, middleware.CommunityLimit("comment"), handler.CreatePostComment)
		posts.DELETE("/:post_id/comments/:comment_id", middleware.Auth, handler.DeletePostComment)
		posts.POST("", middleware.Auth, handler.CreatePost)
		posts.PATCH("/:post_id", middleware.Auth, handler.EditPost)
		posts.POST("/:post_id/publish", middleware.Auth, handler.PublishPost)
		posts.DELETE("/:post_id", middleware.Auth, handler.DeletePost)

		comments := apiV1Group.Group("/comments", middleware.Auth)
		comments.GET("/mine", handler.ListMyComments)
		comments.PATCH("/:comment_id", middleware.CommunityLimit("comment"), handler.EditComment)
		comments.PUT("/:comment_id/like", middleware.CommunityLimit("interaction"), handler.LikeComment)
		comments.DELETE("/:comment_id/like", middleware.CommunityLimit("interaction"), handler.LikeComment)
		comments.POST("/:comment_id/reports", middleware.CommunityLimit("report"), handler.ReportComment)
		me := apiV1Group.Group("/me", middleware.Auth)
		me.GET("/favorite-folders", handler.FavoriteFolders)
		me.POST("/favorite-folders", middleware.CommunityLimit("interaction"), handler.FavoriteFolders)
		me.PATCH("/favorite-folders/:folder_id", middleware.CommunityLimit("interaction"), handler.ChangeFavoriteFolder)
		me.DELETE("/favorite-folders/:folder_id", middleware.CommunityLimit("interaction"), handler.ChangeFavoriteFolder)
		me.GET("/favorite-folders/:folder_id/items", handler.FavoriteItems)
		me.PUT("/favorite-folders/:folder_id/items/:video_id", middleware.CommunityLimit("interaction"), handler.SetFavorite)
		me.DELETE("/favorite-folders/:folder_id/items/:video_id", middleware.CommunityLimit("interaction"), handler.SetFavorite)
		me.GET("/favorites/:video_id", handler.FavoriteMembership)
		me.GET("/notifications", handler.Notifications)
		me.GET("/notifications/unread", handler.UnreadNotifications)
		me.POST("/notifications/read", handler.ReadNotification)
		me.PUT("/notifications/:notification_id/read", handler.ReadNotification)
		apiV1Group.POST("/reports", middleware.Auth, handler.SubmitReport)
		videos := apiV1Group.Group("/videos")
		admin := apiV1Group.Group("/admin", middleware.Auth, middleware.RequireAdmin)
		admin.GET("/reports", handler.Reports)
		admin.POST("/reports/:report_id/resolve", handler.ResolveReport)
		admin.GET("/reports/:report_id/history", handler.ReportHistory)
		admin.GET("/comments", handler.ListReviewComments)
		admin.GET("/comments/:comment_id/history", handler.CommentHistory)
		admin.POST("/comments/:comment_id/review", handler.ReviewComment)
		admin.POST("/comments/rebuild-likes", handler.RebuildCommentLikes)
		admin.GET("/comment-reports", handler.ListCommentReports)
		admin.POST("/comment-reports/:report_id/resolve", handler.ResolveCommentReport)
		admin.GET("/videos", handler.ListReviewVideos)
		admin.GET("/videos/:video_id", handler.GetReviewVideo)
		admin.GET("/videos/:video_id/media", handler.PreviewReviewVideo)
		admin.HEAD("/videos/:video_id/media", handler.PreviewReviewVideo)
		admin.POST("/videos/:video_id/review", handler.ReviewVideo)
		{
			videos.GET("", handler.ListVideos)
			videos.GET("/mine", middleware.Auth, handler.ListMyVideos)
			videos.GET("/mine/:video_id", middleware.Auth, handler.GetMyVideo)
			videos.GET("/:video_id/like", handler.VideoLike)
			videos.PUT("/:video_id/like", middleware.Auth, middleware.CommunityLimit("interaction"), handler.VideoLike)
			videos.DELETE("/:video_id/like", middleware.Auth, middleware.CommunityLimit("interaction"), handler.VideoLike)
			videos.GET("/:video_id", handler.GetVideo)
			videos.GET("/:video_id/danmakus", handler.VideoDanmakus)
			videos.POST("/:video_id/danmakus", middleware.Auth, middleware.CommunityLimit("danmaku"), handler.VideoDanmakus)
			videos.DELETE("/:video_id/danmakus/:danmaku_id", middleware.Auth, middleware.CommunityLimit("danmaku"), handler.DeleteVideoDanmaku)
			videos.GET("/:video_id/comments", handler.ListVideoComments)
			videos.GET("/:video_id/comments/:comment_id/replies", handler.ListVideoCommentReplies)
			videos.POST("/:video_id/comments", middleware.Auth, middleware.CommunityLimit("comment"), handler.CreateVideoComment)
			videos.DELETE("/:video_id/comments/:comment_id", middleware.Auth, handler.DeleteVideoComment)
			videos.GET("/:video_id/media", handler.ServeVideoMedia)
			videos.HEAD("/:video_id/media", handler.ServeVideoMedia)

			videos.POST("", middleware.Auth, handler.CreateVideo)
			videos.POST("/:video_id/upload", middleware.Auth, handler.UploadVideo)
			videos.POST("/:video_id/publish", middleware.Auth, handler.PublishVideo)
			videos.PATCH("/:video_id", middleware.Auth, handler.EditVideo)
			videos.POST("/:video_id/unpublish", middleware.Auth, handler.UnpublishVideo)
			videos.DELETE("/:video_id", middleware.Auth, handler.DeleteVideo)
		}

	}

}
