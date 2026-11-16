package repo

import (
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"live-stream/internal/model"
	"testing"
	"time"
)

func TestLivePublishTransition(t *testing.T) {
	now := time.Now()
	for _, tt := range []struct {
		state, oldClient, newClient string
		expires                     time.Time
		want                        bool
	}{
		{"pending", "", "one", now.Add(time.Minute), true},
		{"pending", "", "one", now.Add(-time.Minute), false},
		{"live", "one", "one", now, true},
		{"live", "one", "two", now, false},
		{"ended", "one", "one", now, false},
		{"pending", "", "", now.Add(time.Minute), false},
	} {
		if got := CanPublishLive(model.LiveSession{Status: tt.state, ClientID: tt.oldClient, ExpiresAt: tt.expires}, tt.newClient, now); got != tt.want {
			t.Fatalf("%+v got %v", tt, got)
		}
	}
}

func TestLiveCallbacksSerializeAndIsolateSessions(t *testing.T) {
	for _, scenario := range []string{"publish", "outbox failure", "duplicate publish", "late publish", "old stop", "wrong client stop", "stop before publish"} {
		t.Run(scenario, func(t *testing.T) {
			mock := communityDB(t)
			status := "pending"
			client := ""
			current := uint64(10)
			publish := true
			switch scenario {
			case "duplicate publish":
				status = "live"
				client = "one"
			case "late publish":
				status = "ended"
				current = 11
			case "old stop":
				status = "live"
				client = "one"
				current = 11
				publish = false
			case "wrong client stop":
				status = "live"
				client = "another"
				publish = false
			case "stop before publish":
				publish = false
			}
			sessionRows := func() *sqlmock.Rows {
				return sqlmock.NewRows([]string{"id", "room_id", "owner_id", "stream", "status", "client_id", "expires_at"}).AddRow(10, 1, 7, "stream", status, client, time.Now().Add(time.Minute))
			}
			mock.ExpectQuery("SELECT .* FROM `live_sessions` WHERE stream").WillReturnRows(sessionRows())
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT .* FROM `rooms`.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"room_id", "owner_id", "live_session_id"}).AddRow(1, 7, current))
			mock.ExpectQuery("SELECT .* FROM `live_sessions`.*FOR UPDATE").WillReturnRows(sessionRows())
			switch scenario {
			case "late publish":
				mock.ExpectRollback()
			case "duplicate publish", "wrong client stop":
				mock.ExpectCommit()
			default:
				mock.ExpectExec("UPDATE `live_sessions`").WillReturnResult(sqlmock.NewResult(0, 1))
				if scenario != "old stop" {
					mock.ExpectExec("UPDATE `rooms`").WillReturnResult(sqlmock.NewResult(0, 1))
				}
				if publish {
					insert := mock.ExpectExec("INSERT INTO `outbox_events`")
					if scenario == "outbox failure" {
						insert.WillReturnError(errors.New("disk failure"))
						mock.ExpectRollback()
					} else {
						insert.WillReturnResult(sqlmock.NewResult(1, 1))
						mock.ExpectCommit()
					}
				} else {
					mock.ExpectCommit()
				}
			}
			_, err := ApplyLiveCallback(context.Background(), "stream", "one", publish, "/live/stream.m3u8")
			wantError := scenario == "outbox failure" || scenario == "late publish"
			if (err != nil) != wantError {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestPrepareLiveRequiresRoomOwner(t *testing.T) {
	mock := communityDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `rooms` WHERE room_id = \\? AND owner_id = \\?.*FOR UPDATE").WithArgs(uint64(1), uint64(99), 1).WillReturnRows(sqlmock.NewRows([]string{"room_id"}))
	mock.ExpectRollback()
	if _, err := PrepareLive(context.Background(), 1, 99, "wrong owner"); err == nil {
		t.Fatal("other owner received credentials")
	}
}

func TestPrepareLiveReusesPendingAndCreatesNewAfterEnd(t *testing.T) {
	for _, status := range []string{"pending", "ended"} {
		t.Run(status, func(t *testing.T) {
			mock := communityDB(t)
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT .* FROM `rooms`.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"room_id", "owner_id", "live_session_id"}).AddRow(1, 7, 10))
			mock.ExpectQuery("SELECT .* FROM `live_sessions`").WillReturnRows(sqlmock.NewRows([]string{"id", "room_id", "owner_id", "stream", "status", "expires_at"}).AddRow(10, 1, 7, "old-stream", status, time.Now().Add(time.Minute)))
			if status == "ended" {
				mock.ExpectExec("INSERT INTO `live_sessions`").WillReturnResult(sqlmock.NewResult(11, 1))
				mock.ExpectExec("UPDATE `rooms`").WillReturnResult(sqlmock.NewResult(0, 1))
			}
			mock.ExpectCommit()
			s, err := PrepareLive(context.Background(), 1, 7, "test")
			if err != nil {
				t.Fatal(err)
			}
			if status == "pending" && s.ID != 10 {
				t.Fatal("retry created another session")
			}
			if status == "ended" && (s.ID != 11 || s.Stream == "old-stream" || s.Status != "pending") {
				t.Fatal("ended stream reused")
			}
		})
	}
}

func TestEndLiveRequiresOwner(t *testing.T) {
	mock := communityDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `rooms` WHERE room_id = \\? AND owner_id = \\?.*FOR UPDATE").WithArgs(uint64(1), uint64(99), 1).WillReturnRows(sqlmock.NewRows([]string{"room_id"}))
	mock.ExpectRollback()
	if _, err := EndLive(context.Background(), 1, 99); err == nil {
		t.Fatal("other user ended room")
	}
}
