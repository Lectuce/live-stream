package rousters

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestCommunityRequiresSession(t *testing.T) {
	r := gin.New()
	SetUpRousters(r, nil)
	for _, route := range []struct{ method, path string }{{"GET", "/api/v1/feeds/following"}, {"POST", "/api/v1/posts"}, {"POST", "/api/v1/posts/images"}, {"GET", "/api/v1/posts/mine"}, {"GET", "/api/v1/posts/mine/1"}, {"PATCH", "/api/v1/posts/1"}, {"POST", "/api/v1/posts/1/publish"}, {"DELETE", "/api/v1/posts/1"}} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(route.method, route.path, nil))
		if w.Code != 401 {
			t.Fatalf("%s: %d", route.path, w.Code)
		}
	}
}
