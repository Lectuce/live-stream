package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestEditVideoRejectsUnknownFieldsAndMalformedJSON(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", uint64(1)); c.Next() })
	r.PATCH("/videos/:video_id", EditVideo)
	for _, body := range []string{`{"title":"x","audit_status":"approved"}`, `{"owner_id":"2"}`, `{"title":123}`, `{} {}`, `{`} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("PATCH", "/videos/1", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatalf("body=%s status=%d", body, w.Code)
		}
	}
}

func TestVideoPageRejectsInvalidParameters(t *testing.T) {
	for _, query := range []string{"limit=0", "limit=51", "limit=abc", "cursor=0", "cursor=-1", "cursor=invalid", "cursor=18446744073709551616"} {
		t.Run(query, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/?"+query, nil)
			if _, _, ok := parseVideoPage(c); ok || w.Code != 400 {
				t.Fatalf("invalid pagination accepted: status=%d", w.Code)
			}
		})
	}
}

func TestVideoPagePreservesLargeCursor(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/?cursor=9007199254740993", nil)
	id, limit, ok := parseVideoPage(c)
	if !ok || id != 9007199254740993 || limit != 20 {
		t.Fatalf("unexpected pagination: id=%d limit=%d ok=%v", id, limit, ok)
	}
}
