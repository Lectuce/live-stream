package handler

import (
	"github.com/gin-gonic/gin"
	"live-stream/internal/dao"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscoveryRejectsInvalidInputBeforeSQL(t *testing.T) {
	old := dao.SC
	dao.SC = nil
	t.Cleanup(func() { dao.SC = old })
	r := gin.New()
	r.GET("/feed", VideoDiscovery)
	r.GET("/search", SearchDiscovery)
	r.POST("/videos/:video_id/behavior", VideoBehavior)
	r.PUT("/videos/:video_id/play-sessions", VideoPlaySession)
	for _, url := range []string{"/feed?limit=0", "/feed?sort=unknown", "/feed?category=unknown", "/feed?cursor=invalid", "/search?q=x&type=private", "/search?q="} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
		if w.Code != 400 {
			t.Fatal(url, w.Code, w.Body.String())
		}
	}
	for _, route := range []struct{ method, path string }{{"POST", "/videos/1/behavior"}, {"PUT", "/videos/1/play-sessions"}} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`)))
		if w.Code != 401 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
