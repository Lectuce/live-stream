package middleware

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"live-stream/internal/model"
	"net/http/httptest"
	"testing"
)

func TestRequireAdmin(t *testing.T) {
	for _, tc := range []struct {
		name, role string
		logged     bool
		err        error
		want       int
	}{
		{name: "anonymous", want: 401}, {name: "ordinary user", logged: true, role: "user", want: 403},
		{name: "admin", logged: true, role: "admin", want: 204}, {name: "removed user", logged: true, err: gorm.ErrRecordNotFound, want: 401},
		{name: "database failure", logged: true, err: errors.New("offline"), want: 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				if tc.logged {
					c.Set("user_id", uint64(42))
				}
				requireAdmin(c, func(ctx context.Context, id uint64) (*model.User, error) { return &model.User{Role: tc.role}, tc.err })
			})
			r.GET("/", func(c *gin.Context) { c.Status(204) })
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d", w.Code, tc.want)
			}
		})
	}
}
