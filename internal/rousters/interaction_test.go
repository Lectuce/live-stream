package rousters

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestInteractionRoutesRequireSession(t *testing.T) {
	r := gin.New()
	SetUpRousters(r, nil)
	for _, tc := range []struct{ method, path string }{
		{"PUT", "/api/v1/videos/7/like"}, {"DELETE", "/api/v1/posts/7/like"}, {"GET", "/api/v1/me/favorite-folders"}, {"POST", "/api/v1/me/favorite-folders"}, {"PATCH", "/api/v1/me/favorite-folders/1"}, {"DELETE", "/api/v1/me/favorite-folders/1"}, {"GET", "/api/v1/me/favorite-folders/1/items"}, {"PUT", "/api/v1/me/favorite-folders/1/items/7"}, {"DELETE", "/api/v1/me/favorite-folders/1/items/7"}, {"GET", "/api/v1/me/favorites/7"}, {"GET", "/api/v1/me/notifications"}, {"GET", "/api/v1/me/notifications/unread"}, {"PUT", "/api/v1/me/notifications/1/read"}, {"POST", "/api/v1/me/notifications/read"}, {"POST", "/api/v1/reports"}, {"GET", "/api/v1/admin/reports"}, {"POST", "/api/v1/admin/reports/1/resolve"}, {"GET", "/api/v1/admin/reports/1/history"},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != 401 {
			t.Fatalf("%s %s: %d", tc.method, tc.path, w.Code)
		}
	}
}
