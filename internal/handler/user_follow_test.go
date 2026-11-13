package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"live-stream/internal/dao"
	"live-stream/internal/model"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func followTestDB(t *testing.T) sqlmock.Sqlmock {
	t.Helper()
	conn, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: conn, SkipInitializeWithVersion: true}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	previous := dao.SC
	dao.SC = &model.StorageContainer{Db: db}
	t.Cleanup(func() {
		dao.SC = previous
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
		conn.Close()
	})
	return mock
}

func followTestRouter(actor uint64) *gin.Engine {
	r := gin.New()
	if actor != 0 {
		r.Use(func(c *gin.Context) { c.Set("user_id", actor); c.Next() })
	}
	r.GET("/users/:user_id", GetPublicUser)
	r.GET("/users/:user_id/follow", UserFollow)
	r.PUT("/users/:user_id/follow", UserFollow)
	r.DELETE("/users/:user_id/follow", UserFollow)
	return r
}

func TestFollowRejectsInvalidIdentityAndOrigin(t *testing.T) {
	for _, tc := range []struct {
		name, method, target, origin string
		actor                        uint64
		status                       int
	}{
		{"anonymous", "PUT", "2", "", 0, 401},
		{"self", "PUT", "7", "", 7, 400},
		{"invalid id", "PUT", "0", "", 7, 400},
		{"cross origin", "DELETE", "2", "https://untrusted.invalid", 7, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := followTestRouter(tc.actor)
			req := httptest.NewRequest(tc.method, "/users/"+tc.target+"/follow", nil)
			req.Header.Set("Origin", tc.origin)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestFollowRetriesAndSpoofedActor(t *testing.T) {
	mock := followTestDB(t)
	r := followTestRouter(7)
	for i, method := range []string{"PUT", "PUT", "DELETE", "DELETE"} {
		mock.ExpectQuery("SELECT user_id, user_name, avatar_url, bio FROM `users` WHERE user_id = \\? LIMIT \\?").
			WithArgs(uint64(2), 1).WillReturnRows(sqlmock.NewRows([]string{"user_id", "user_name"}).AddRow(2, "author"))
		mock.ExpectBegin()
		if method == "PUT" {
			mock.ExpectExec("INSERT INTO `user_follows` .* ON DUPLICATE KEY UPDATE `id`=`id`").
				WithArgs(uint64(7), uint64(2), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, int64(1-i)))
		} else {
			mock.ExpectExec("DELETE FROM `user_follows` WHERE follower_id = \\? AND followee_id = \\?").
				WithArgs(uint64(7), uint64(2)).WillReturnResult(sqlmock.NewResult(0, int64(3-i)))
		}
		mock.ExpectCommit()
		count := 0
		if method == "PUT" {
			count = 1
		}
		mock.ExpectQuery("SELECT.*EXISTS.*").WithArgs(uint64(7), uint64(2), uint64(2)).
			WillReturnRows(sqlmock.NewRows([]string{"following", "follower_count"}).AddRow(count, count))
		// Forged actor IDs must never replace the session's user 7 in SQL.
		req := httptest.NewRequest(method, "/users/2/follow?follower_id=999", strings.NewReader(`{"follower_id":999}`))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", method, w.Code, w.Body.String())
		}
		if method == "PUT" && !strings.Contains(w.Body.String(), `"following":true`) {
			t.Fatal(w.Body.String())
		}
		if method == "DELETE" && !strings.Contains(w.Body.String(), `"following":false`) {
			t.Fatal(w.Body.String())
		}
	}
}

func TestFollowMissingTarget(t *testing.T) {
	mock := followTestDB(t)
	mock.ExpectQuery("SELECT user_id, user_name, avatar_url, bio FROM `users`").
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}))
	w := httptest.NewRecorder()
	followTestRouter(7).ServeHTTP(w, httptest.NewRequest("PUT", "/users/999/follow", nil))
	if w.Code != 404 {
		t.Fatalf("%d: %s", w.Code, w.Body.String())
	}
}

func TestPublicProfileExcludesPrivateFields(t *testing.T) {
	mock := followTestDB(t)
	mock.ExpectQuery("SELECT user_id, user_name, avatar_url, bio FROM `users`").
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "user_name", "avatar_url", "bio"}).AddRow(2, "author", "", "bio"))
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `user_follows` WHERE followee_id = \\?").WithArgs(uint64(2)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `user_follows` WHERE follower_id = \\?").WithArgs(uint64(2)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(4))
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `videos` WHERE .*owner_id.*publish_status.*process_status.*audit_status.*deleted_at.*IS NULL").
		WithArgs(uint64(2), "published", "ready", "approved").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))
	w := httptest.NewRecorder()
	followTestRouter(0).ServeHTTP(w, httptest.NewRequest("GET", "/users/2", nil))
	if w.Code != 200 {
		t.Fatalf("%d: %s", w.Code, w.Body.String())
	}
	for _, field := range []string{"user_email", "password", "role", "created_at", "updated_at"} {
		if strings.Contains(w.Body.String(), field) {
			t.Fatalf("private field %s exposed", field)
		}
	}
	if !strings.Contains(w.Body.String(), `"user_id":"2"`) || !strings.Contains(w.Body.String(), `"video_count":5`) {
		t.Fatal(w.Body.String())
	}
}
