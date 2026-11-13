package rousters

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestOwnerVideoRoutesRequireLogin(t *testing.T) {
	r := gin.New()
	SetUpRousters(r, nil)
	for _, path := range []string{"/api/v1/videos/mine", "/api/v1/videos/mine/123"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 401 {
			t.Fatalf("%s: expected auth middleware (401), got %d: %s", path, w.Code, w.Body.String())
		}
	}
	for _, route := range []struct{ method, path string }{
		{"PATCH", "/api/v1/videos/123"}, {"POST", "/api/v1/videos/123/unpublish"}, {"DELETE", "/api/v1/videos/123"},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(route.method, route.path, nil))
		if w.Code != 401 {
			t.Fatalf("%s %s: expected 401 got %d", route.method, route.path, w.Code)
		}
	}
}
