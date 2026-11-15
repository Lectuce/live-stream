package repo

import (
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"
	"live-stream/internal/dao"
	"live-stream/internal/model"
	"testing"
	"time"
)

func TestContentLikeRetryAndCounterFailure(t *testing.T) {
	for _, scenario := range []string{"new", "duplicate", "cancel", "counter failure"} {
		t.Run(scenario, func(t *testing.T) {
			mock := communityDB(t)
			mock.ExpectBegin()
			expectCommentVideo(mock, true, true)
			mock.ExpectExec("INSERT INTO `content_stats`.*ON DUPLICATE KEY UPDATE").WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectQuery("SELECT .* FROM `content_stats`.*FOR UPDATE").WithArgs("video", uint64(7), 1).WillReturnRows(sqlmock.NewRows([]string{"target_type", "target_id", "like_count"}).AddRow("video", 7, 10))
			exists := 0
			if scenario == "duplicate" || scenario == "cancel" {
				exists = 1
			}
			mock.ExpectQuery("SELECT count.* FROM `content_likes`.*").WithArgs("video", uint64(7), uint64(9)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(exists))
			count := int64(10)
			if scenario == "new" || scenario == "counter failure" {
				mock.ExpectExec("INSERT INTO `content_likes`").WithArgs("video", uint64(7), uint64(9), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
				count++
			}
			if scenario == "cancel" {
				mock.ExpectExec("DELETE FROM `content_likes`.*").WithArgs("video", uint64(7), uint64(9)).WillReturnResult(sqlmock.NewResult(0, 1))
				count--
			}
			if scenario != "duplicate" {
				e := mock.ExpectExec("UPDATE `content_stats` SET `like_count`=.*").WithArgs(count, "video", uint64(7))
				if scenario == "counter failure" {
					e.WillReturnError(errors.New("disk full"))
				} else {
					e.WillReturnResult(sqlmock.NewResult(0, 1))
				}
			}
			if scenario == "counter failure" {
				mock.ExpectRollback()
			} else {
				mock.ExpectCommit()
			}
			state, err := SetContentLike(context.Background(), "video", 7, 9, scenario != "cancel")
			if scenario == "counter failure" {
				if err == nil {
					t.Fatal("relation committed without counter")
				}
			} else if err != nil || state.LikeCount != count {
				t.Fatal(state, err)
			}
		})
	}
}

func TestFavoriteOwnershipAndRemovalOfUnavailableVideo(t *testing.T) {
	for _, wrongOwner := range []bool{true, false} {
		t.Run(map[bool]string{true: "other owner", false: "remove unavailable"}[wrongOwner], func(t *testing.T) {
			mock := communityDB(t)
			mock.ExpectBegin()
			rows := sqlmock.NewRows([]string{"id", "owner_id"})
			if !wrongOwner {
				rows.AddRow(3, 9)
			}
			mock.ExpectQuery("SELECT .* FROM `favorite_folders` WHERE id = .* AND owner_id = .*FOR UPDATE").WithArgs(uint64(3), uint64(9), 1).WillReturnRows(rows)
			if wrongOwner {
				mock.ExpectRollback()
			} else {
				mock.ExpectExec("DELETE FROM `favorite_items` WHERE folder_id = .* AND video_id = .*").WithArgs(uint64(3), uint64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			}
			err := SetFavorite(context.Background(), 3, 7, 9, false)
			if wrongOwner && !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatal(err)
			}
			if !wrongOwner && err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestFavoriteListingMasksPrivateVideoFields(t *testing.T) {
	mock := communityDB(t)
	mock.ExpectQuery("SELECT .* FROM `favorite_folders` WHERE id = .* AND owner_id = .*").WithArgs(uint64(3), uint64(9), 1).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(3))
	mock.ExpectQuery("SELECT i.*COALESCE.*FROM favorite_items AS i JOIN favorite_folders AS f ON f.id = i.folder_id AND f.owner_id = .*LEFT JOIN videos AS v ON.*v.deleted_at IS NULL.*publish_status = 'published'.*audit_status = 'approved'.*i.id <.*ORDER BY i.id DESC LIMIT").WithArgs(uint64(9), uint64(3), uint64(80), 21).WillReturnRows(sqlmock.NewRows([]string{"id", "video_id", "title", "cover_url", "available"}).AddRow(5, 7, "视频不可用", "", false))
	rows, err := ListFavorites(context.Background(), 3, 9, 80, 21)
	if err != nil || len(rows) != 1 || rows[0].Available || rows[0].CoverURL != "" {
		t.Fatal(rows, err)
	}
}
func TestNotificationOutboxRollbackAndDuplicateDelivery(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "duplicate event", true: "ack failure"}[fail], func(t *testing.T) {
			mock := communityDB(t)
			mock.ExpectBegin()
			now := time.Now()
			mock.ExpectQuery("SELECT .* FROM `notification_outboxes` WHERE delivered_at IS NULL ORDER BY id ASC LIMIT .*FOR UPDATE SKIP LOCKED").WithArgs(100).WillReturnRows(sqlmock.NewRows([]string{"id", "event_key", "recipient_id", "actor_id", "kind", "target_type", "target_id", "created_at"}).AddRow(1, "follow:1", 7, 9, "follow", "user", 9, now))
			mock.ExpectExec("INSERT INTO `notifications`.*ON DUPLICATE KEY UPDATE").WithArgs("follow:1", uint64(7), uint64(9), "follow", "user", uint64(9), now, nil).WillReturnResult(sqlmock.NewResult(0, 0))
			e := mock.ExpectExec("UPDATE `notification_outboxes` SET `delivered_at`=.*WHERE `id` = .*").WithArgs(sqlmock.AnyArg(), uint64(1))
			if fail {
				e.WillReturnError(errors.New("ack failure"))
				mock.ExpectRollback()
			} else {
				e.WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			}
			n, err := DeliverNotifications(context.Background())
			if fail && err == nil {
				t.Fatal("partial commit")
			}
			if !fail && (err != nil || n != 1) {
				t.Fatal(n, err)
			}
		})
	}
}
func TestSelfNotificationDoesNotWrite(t *testing.T) {
	communityDB(t)
	if err := QueueNotification(dao.SC.Db, "same", "follow", "user", 9, 9, 9); err != nil {
		t.Fatal(err)
	}
}

func TestApprovedReplyQueuesReviewAndReplyInSameTransaction(t *testing.T) {
	mock := communityDB(t)
	mock.ExpectBegin()
	row := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{"id", "target_type", "target_id", "root_id", "user_id", "reply_to_user_id", "version", "audit_status"}).AddRow(5, "post", 7, 2, 9, 8, 3, "pending")
	}
	mock.ExpectQuery("SELECT .* FROM `comments`.*LIMIT").WithArgs(uint64(5), 1).WillReturnRows(row())
	mock.ExpectQuery("SELECT .* FROM `comments` WHERE id = .*root_id = 0.*FOR UPDATE").WithArgs(uint64(2), "post", uint64(7), 1).WillReturnRows(sqlmock.NewRows([]string{"id", "audit_status"}).AddRow(2, "approved"))
	mock.ExpectQuery("SELECT .* FROM `comments`.*FOR UPDATE").WithArgs(uint64(5), 1).WillReturnRows(row())
	mock.ExpectExec("INSERT INTO `comment_reviews`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE `comments` SET.*").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO `notification_outboxes`").WithArgs("comment.review:5:3:approved", uint64(9), uint64(42), "comment.review", "comment", uint64(5), sqlmock.AnyArg(), nil).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO `notification_outboxes`.*ON DUPLICATE KEY UPDATE").WithArgs("comment.reply:5", uint64(8), uint64(9), "comment.reply", "comment", uint64(5), sqlmock.AnyArg(), nil).WillReturnResult(sqlmock.NewResult(2, 1))
	mock.ExpectCommit()
	if err := ReviewComment(context.Background(), 5, 42, model.ReviewCommentRequest{Version: 3, Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
}
func TestNotificationReadOwnershipAndWatermark(t *testing.T) {
	mock := communityDB(t)
	mock.ExpectQuery("SELECT `id` FROM `notifications` WHERE id = .* AND recipient_id = .*").WithArgs(uint64(8), uint64(9), 1).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if err := ReadNotification(context.Background(), 9, 8, false); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal(err)
	}
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `notifications` SET `read_at`=.*WHERE .*recipient_id = .*read_at IS NULL.*id <= .*").WithArgs(sqlmock.AnyArg(), uint64(9), uint64(8)).WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectCommit()
	if err := ReadNotification(context.Background(), 9, 8, true); err != nil {
		t.Fatal(err)
	}
}
func TestReportOldVersionCannotHideEditedPost(t *testing.T) {
	mock := communityDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `reports`.*FOR UPDATE").WithArgs(uint64(1), 1).WillReturnRows(sqlmock.NewRows([]string{"id", "target_type", "target_id", "status"}).AddRow(1, "post", "7", "pending"))
	mock.ExpectQuery("SELECT .* FROM `posts`.*FOR UPDATE").WithArgs(uint64(7), 1).WillReturnRows(sqlmock.NewRows([]string{"post_id", "version", "content"}).AddRow(7, 3, "new text"))
	mock.ExpectRollback()
	if err := ResolveReport(context.Background(), 1, 9, 2, "hidden", "spam"); !errors.Is(err, ErrCommentConflict) {
		t.Fatal(err)
	}
}
func TestReportAuditFailureRollsBackModeration(t *testing.T) {
	mock := communityDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `reports`.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"id", "target_type", "target_id", "status"}).AddRow(1, "post", "7", "pending"))
	mock.ExpectQuery("SELECT .* FROM `posts`.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"post_id", "version"}).AddRow(7, 3))
	mock.ExpectExec("UPDATE `posts` SET.*").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO `report_actions`").WillReturnError(errors.New("audit unavailable"))
	mock.ExpectRollback()
	if err := ResolveReport(context.Background(), 1, 9, 3, "hidden", "spam"); err == nil {
		t.Fatal("moderation without audit")
	}
}
func TestCachedDanmakuRedactionPreservesSequence(t *testing.T) {
	mock := communityDB(t)
	now := time.Now()
	mock.ExpectQuery("SELECT message_id,hidden_at FROM `danmas` WHERE message_id IN .* AND hidden_at IS NOT NULL").WithArgs("a", "b").WillReturnRows(sqlmock.NewRows([]string{"message_id", "hidden_at"}).AddRow("a", now))
	rows := []model.Danma{{MessageID: "a", RoomSeq: 10, Content: "hidden"}, {MessageID: "b", RoomSeq: 11, Content: "visible"}}
	if err := RedactHiddenDanmas(context.Background(), rows); err != nil {
		t.Fatal(err)
	}
	if rows[0].Content == "hidden" || rows[0].RoomSeq != 10 || rows[1].Content != "visible" {
		t.Fatal(rows)
	}
}
