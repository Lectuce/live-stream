package rousters

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestFollowRoutesRequireAuthentication(t *testing.T) {
	r := gin.New()
	SetUpRousters(r, nil)
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, "/api/v1/users/2/follow", nil))
		if w.Code != 401 {
			t.Fatalf("%s: %d %s", method, w.Code, w.Body.String())
		}
	}
}
