package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestWatchEndpointsRequireLoginAndRejectForgedPayloads(t *testing.T) {
	for _, tc := range []struct {
		login                bool
		method, suffix, body string
		want                 int
	}{
		{false, "GET", "", "", 401},
		{false, "PUT", "/progress", `{}`, 401},
		{true, "PUT", "/progress", `{"user_id":"2"}`, 400},
		{true, "PUT", "/progress", `{"session_id":"6f8d62dc-9e90-4b51-9265-8741fca52903","session_version":"1","history_epoch":"1","seq":0,"position_ms":0}`, 400},
		{true, "POST", "/session", `{"session_id":"invalid","session_version":"1","history_epoch":"1"}`, 400},
	} {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			if tc.login {
				c.Set("user_id", uint64(9))
			}
			c.Next()
		})
		r.GET("/history/:video_id", WatchVideoHistory)
		r.POST("/history/:video_id/session", WatchSession)
		r.PUT("/history/:video_id/progress", WatchProgress)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(tc.method, "/history/7"+tc.suffix, strings.NewReader(tc.body)))
		if w.Code != tc.want {
			t.Fatal(tc, w.Code, w.Body.String())
		}
	}
}
