package rousters

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestCommentMutationRoutesRequireSession(t *testing.T) {
	r := gin.New()
	SetUpRousters(r, nil)
	for _, tc := range []struct{ method, path string }{{"POST", "/api/v1/videos/7/comments"}, {"DELETE", "/api/v1/videos/7/comments/1"}} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != 401 {
			t.Fatalf("%s %d", tc.path, w.Code)
		}
	}
}
