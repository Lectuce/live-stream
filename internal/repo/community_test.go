package repo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"live-stream/internal/dao"
	"live-stream/internal/model"
)

func communityDB(t *testing.T) sqlmock.Sqlmock {
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

func TestVideoPublishAndOutboxAtomic(t *testing.T) {
	for _, scenario := range []string{"publish", "outbox failure", "retry published", "not approved", "deleted or wrong owner"} {
		t.Run(scenario, func(t *testing.T) {
			mock := communityDB(t)
			mock.ExpectBegin()
			rows := sqlmock.NewRows([]string{"video_id", "owner_id", "publish_status", "process_status", "audit_status", "play_url"})
			status, audit := "draft", "approved"
			if scenario == "retry published" {
				status = "published"
			}
			if scenario == "not approved" {
				audit = "pending"
			}
			if scenario != "deleted or wrong owner" {
				rows.AddRow(1, 7, status, "ready", audit, "/media")
			}
			mock.ExpectQuery("SELECT .* FROM `videos` WHERE .*video_id = \\? AND owner_id = \\?.*`videos`.`deleted_at` IS NULL.*FOR UPDATE").WithArgs(uint64(1), uint64(7), 1).WillReturnRows(rows)
			switch scenario {
			case "deleted or wrong owner":
				mock.ExpectRollback()
			case "not approved":
				mock.ExpectCommit()
			default:
				if scenario != "retry published" {
					mock.ExpectExec("UPDATE `videos` SET .*publish_status.*published_at.* WHERE .*deleted_at.*").WillReturnResult(sqlmock.NewResult(0, 1))
				}
				insert := mock.ExpectExec("INSERT INTO `outbox_events` .*ON DUPLICATE KEY UPDATE `id`=`id`")
				if scenario == "outbox failure" {
					insert.WillReturnError(errors.New("disk error"))
					mock.ExpectRollback()
				} else {
					insert.WillReturnResult(sqlmock.NewResult(1, 1))
					mock.ExpectCommit()
				}
			}
			count, err := PublishVideo(context.Background(), 1, 7)
			wantError := scenario == "outbox failure" || scenario == "deleted or wrong owner"
			if (err != nil) != wantError {
				t.Fatalf("error=%v", err)
			}
			if !wantError && scenario != "not approved" && count != 1 {
				t.Fatalf("rows=%d", count)
			}
		})
	}
}

func TestProjectDuplicateIsNoOp(t *testing.T) {
	mock := communityDB(t)
	for i := 0; i < 2; i++ {
		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO `feed_events` .*ON DUPLICATE KEY UPDATE `id`=`id`").WillReturnResult(sqlmock.NewResult(1, int64(1-i)))
		mock.ExpectCommit()
		if err := ProjectContentEvent(context.Background(), model.ContentEvent{EventID: "same-event", BusinessKey: "video.published:1", TargetID: 1, ActorID: 7, EventType: "video.published", PublishedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFinishLeaseRequiresToken(t *testing.T) {
	mock := communityDB(t)
	for _, deliveryErr := range []error{nil, errors.New("broker down")} {
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE `outbox_events` SET .* WHERE id = \\? AND status = 'sending' AND lock_token = \\?").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()
		if err := FinishContentEvent(context.Background(), &model.OutboxEvent{ID: 1, LockToken: "old-owner"}, deliveryErr); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFollowingFiltersBeforeLimitAndCursor(t *testing.T) {
	mock := communityDB(t)
	now := time.Now().Truncate(time.Microsecond)
	mock.ExpectQuery("SELECT .* FROM feed_events AS e JOIN user_follows AS f ON f.followee_id = e.actor_id AND f.follower_id = \\?.*v.publish_status = 'published'.*v.process_status = 'ready'.*v.audit_status = 'approved'.*v.deleted_at IS NULL.*p.status = 'published'.*p.deleted_at IS NULL.*AND.*e.published_at < \\?.*e.published_at = \\?.*e.id < \\?.*ORDER BY e.published_at DESC, e.id DESC LIMIT \\?").WithArgs(uint64(8), now, now, uint64(9), 21).
		WillReturnRows(sqlmock.NewRows([]string{"id", "event_id"}))
	items, err := SelectFollowing(context.Background(), 8, now, 9, 21)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatal(items)
	}
}

func TestPostEditRejectsStaleVersionAndForeignImages(t *testing.T) {
	for _, scenario := range []string{"stale", "foreign image", "attached elsewhere", "missing image"} {
		t.Run(scenario, func(t *testing.T) {
			mock := communityDB(t)
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT .* FROM `posts`.*owner_id.*deleted_at.*FOR UPDATE").WithArgs(uint64(3), uint64(7), 1).WillReturnRows(sqlmock.NewRows([]string{"post_id", "owner_id", "version", "status"}).AddRow(3, 7, 2, "draft"))
			version := uint64(2)
			if scenario == "stale" {
				version = 1
			} else {
				rows := sqlmock.NewRows([]string{"id", "owner_id", "post_id"})
				if scenario == "foreign image" {
					rows.AddRow("img", 8, 0)
				}
				if scenario == "attached elsewhere" {
					rows.AddRow("img", 7, 99)
				}
				mock.ExpectQuery("SELECT .* FROM `post_images`.*FOR UPDATE").WillReturnRows(rows)
			}
			mock.ExpectRollback()
			err := EditPost(context.Background(), 3, 7, model.EditPostRequest{Version: version, Content: "hello", ImageIDs: []string{"img"}})
			if scenario == "stale" && !errors.Is(err, ErrPostConflict) {
				t.Fatal(err)
			}
			if scenario != "stale" && !errors.Is(err, ErrPostImages) {
				t.Fatal(err)
			}
		})
	}
}

func TestPostPublishingRollsBackOnOutboxFailure(t *testing.T) {
	mock := communityDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `posts`.*owner_id.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"post_id", "owner_id", "version", "content", "status"}).AddRow(3, 7, 1, "hello", "draft"))
	mock.ExpectQuery("SELECT count.*FROM `post_images`").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectExec("UPDATE `posts` SET.*").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO `outbox_events`.*").WillReturnError(errors.New("outbox unavailable"))
	mock.ExpectRollback()
	if err := PublishPost(context.Background(), 3, 7, 1); err == nil {
		t.Fatal("expected rollback")
	}
}
