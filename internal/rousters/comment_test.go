package rousters

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestCommentMutationRoutesRequireSession(t *testing.T) {
	r := gin.New()
	SetUpRousters(r, nil)
	for _, tc := range []struct{ method, path string }{{"POST", "/api/v1/videos/7/comments"}, {"DELETE", "/api/v1/videos/7/comments/1"}, {"POST", "/api/v1/posts/7/comments"}, {"DELETE", "/api/v1/posts/7/comments/1"}, {"PATCH", "/api/v1/comments/1"}, {"PUT", "/api/v1/comments/1/like"}, {"DELETE", "/api/v1/comments/1/like"}, {"POST", "/api/v1/comments/1/reports"}, {"GET", "/api/v1/comments/mine"}, {"GET", "/api/v1/admin/comments"}, {"POST", "/api/v1/admin/comments/1/review"}, {"GET", "/api/v1/admin/comments/1/history"}, {"GET", "/api/v1/admin/comment-reports"}, {"POST", "/api/v1/admin/comment-reports/1/resolve"}, {"POST", "/api/v1/admin/comments/rebuild-likes"}} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != 401 {
			t.Fatalf("%s %d", tc.path, w.Code)
		}
	}
}
