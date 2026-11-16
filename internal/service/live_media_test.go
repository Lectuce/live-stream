package service

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"live-stream/internal/config"
	"live-stream/internal/dao"
	"live-stream/internal/model"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReconcileDoesNotEndLiveRoomsOnIncompleteInventory(t *testing.T) {
	for _, body := range []string{`{"code":1,"streams":[]}`, `{"code":0}`, `not-json`} {
		t.Run(body, func(t *testing.T) {
			conn, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			db, err := gorm.Open(mysql.New(mysql.Config{Conn: conn, SkipInitializeWithVersion: true}), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			previous := dao.SC
			dao.SC = &model.StorageContainer{Db: db}
			defer func() { dao.SC = previous }()
			// Only unused pending credentials may expire; live rows must not be changed.
			mock.ExpectBegin()
			mock.ExpectExec("UPDATE `live_sessions`.*WHERE status = \\? AND expires_at <= \\?").WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectCommit()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer server.Close()
			if err := ReconcileLive(context.Background(), config.LiveMedia{API: server.URL}); err == nil {
				t.Fatal("incomplete inventory accepted")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMediaHookAuthentication(t *testing.T) {
	cfg := config.LiveMedia{Secret: strings.Repeat("x", 32)}
	stream := "00000000-0000-4000-8000-000000000001"
	valid := MediaHook{Action: "on_publish", App: "live", Stream: stream, ClientID: "publisher-1", Param: "?token=" + cfg.Token(stream)}
	if !ValidMediaHook(cfg, valid) {
		t.Fatal("valid publish rejected")
	}
	for _, kind := range []string{"token", "stream", "app", "action", "client"} {
		bad := valid
		switch kind {
		case "token":
			bad.Param = "?token=invalid"
		case "stream":
			bad.Stream = "00000000-0000-4000-8000-000000000002"
		case "app":
			bad.App = "private"
		case "action":
			bad.Action = "on_play"
		case "client":
			bad.ClientID = ""
		}
		if ValidMediaHook(cfg, bad) {
			t.Fatalf("accepted invalid %s", kind)
		}
	}
	valid.Action = "on_unpublish"
	if !ValidMediaHook(cfg, valid) {
		t.Fatal("valid unpublish rejected")
	}
}

func TestMediaHookRejectsBeforeDatabaseAccess(t *testing.T) {
	handler := MediaHookHandler(config.LiveMedia{Secret: strings.Repeat("x", 32)})
	for _, body := range []string{`{}`, `{"action":"on_publish","app":"live","stream":"bad"}`, strings.Repeat("x", 17000)} {
		req := httptest.NewRequest("POST", "/hooks/srs", strings.NewReader(body))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 403 {
			t.Fatalf("got %d", w.Code)
		}
	}
}
