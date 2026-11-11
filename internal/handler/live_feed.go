package handler

import (
	"errors"
	"net/http"
	"strconv"

	"live-stream/internal/service"

	"github.com/gin-gonic/gin"
)

func GetHotLiveFeed(c *gin.Context) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil || limit < 1 || limit > 50 {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "limit 必须在 1 到 50 之间",
		})
		return
	}

	page, err := service.GetHotLiveFeed(
		c.Request.Context(),
		c.Query("cursor"),
		limit,
	)

	switch {
	case errors.Is(err, service.ErrInvalidFeedCursor):
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "Feed 游标无效",
		})
	case errors.Is(err, service.ErrFeedSnapshotExpired):
		c.JSON(http.StatusGone, gin.H{
			"message": "Feed 已过期，请刷新",
		})
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "读取 Feed 失败",
		})
	default:
		c.JSON(http.StatusOK, gin.H{
			"data":    page,
			"message": "success",
		})
	}
}
