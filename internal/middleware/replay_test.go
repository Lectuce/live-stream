package middleware

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestReplayLimitBoundsAndReleases(t *testing.T) {
	r := gin.New()
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	r.GET("/:room_id", ReplayLimit(), func(c *gin.Context) { entered <- struct{}{}; <-release; c.Status(200) })
	done := make(chan int, 4)
	for i := 0; i < 4; i++ {
		go func() {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/1", nil))
			done <- w.Code
		}()
	}
	for i := 0; i < 4; i++ {
		<-entered
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/1", nil))
	if w.Code != 429 || w.Header().Get("Retry-After") != "1" {
		t.Fatalf("overload: %d", w.Code)
	}
	close(release)
	for i := 0; i < 4; i++ {
		if <-done != 200 {
			t.Fatal("accepted request failed")
		}
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/1", nil))
	if w.Code != 200 {
		t.Fatal("permit leaked")
	}
}
