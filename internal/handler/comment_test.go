package handler

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCommentRejectsForgedFieldsAndOrigin(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", uint64(9)); c.Next() })
	r.POST("/videos/:video_id/comments", CreateVideoComment)
	for _, tc := range []struct {
		body, origin string
		status       int
	}{
		{`{"content":"hi","request_id":"ae9334e0-e17b-4c58-8a90-95127fbc960e","user_id":"8"}`, "", 400},
		{`{"content":"hi","root_id":"5"}`, "", 400},
		{`{"content":"hi"}`, "https://untrusted.invalid", 403},
		{`{"content":"","request_id":"ae9334e0-e17b-4c58-8a90-95127fbc960e"}`, "", 400},
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/videos/7/comments", strings.NewReader(tc.body))
		req.Header.Set("Origin", tc.origin)
		r.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("%d: %s", w.Code, w.Body.String())
		}
	}
}
