package middleware

import (
	"live-stream/internal/config"
	"live-stream/internal/model"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCORSUsesServerOriginAllowlist(t *testing.T) {
	previous := config.GlobalConfig
	config.GlobalConfig = &model.Config{
		Server: model.ServerConfig{AllowedOrigins: []string{"http://localhost:5173"}},
	}
	t.Cleanup(func() { config.GlobalConfig = previous })

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(CORS())
	router.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	request := httptest.NewRequest(http.MethodGet, "/test", nil)
	request.Header.Set("Origin", "http://localhost:5173")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("allow origin=%q", got)
	}

	request = httptest.NewRequest(http.MethodOptions, "/test", nil)
	request.Header.Set("Origin", "https://not-allowed.example")
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("unknown-origin preflight status=%d, want 403", recorder.Code)
	}
}
