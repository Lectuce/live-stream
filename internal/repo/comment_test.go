package repo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"
	"live-stream/internal/model"
)

const commentRequestID = "ae9334e0-e17b-4c58-8a90-95127fbc960e"

func expectCommentVideo(mock sqlmock.Sqlmock, exists, lock bool) {
	query := "SELECT `video_id` FROM `videos` WHERE .*publish_status = 'published'.*process_status = 'ready'.*audit_status = 'approved'.*deleted_at.*LIMIT \\?"
	if lock {
		query += " FOR SHARE"
	}
	rows := sqlmock.NewRows([]string{"video_id"})
	if exists {
		rows.AddRow(7)
	}
	mock.ExpectQuery(query).WithArgs(uint64(7), 1).WillReturnRows(rows)
}

func expectNoPriorComment(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("SELECT .* FROM `comments` WHERE user_id = \\? AND request_id = \\? LIMIT \\?").WithArgs(uint64(9), commentRequestID, 1).WillReturnRows(sqlmock.NewRows([]string{"id"}))
}

func TestCreateCommentRequiresPublicVideo(t *testing.T) {
	mock := communityDB(t)
	mock.ExpectBegin()
	expectCommentVideo(mock, false, true)
	mock.ExpectRollback()
	_, err := CreateVideoComment(context.Background(), 7, 9, model.CreateCommentRequest{Content: "hello", RequestID: commentRequestID})
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal(err)
	}
}

func TestCreateCommentDerivesRootAndReplyUser(t *testing.T) {
	mock := communityDB(t)
	mock.ExpectBegin()
	expectCommentVideo(mock, true, true)
	expectNoPriorComment(mock)
	mock.ExpectQuery("SELECT .* FROM `comments` WHERE id = \\? AND video_id = \\?.*LIMIT \\?").WithArgs(uint64(22), uint64(7), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "video_id", "root_id", "user_id"}).AddRow(22, 7, 11, 8))
	mock.ExpectQuery("SELECT .* FROM `comments` WHERE id = \\? AND video_id = \\? AND root_id = 0.*FOR UPDATE").WithArgs(uint64(11), uint64(7), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "video_id", "root_id", "user_id"}).AddRow(11, 7, 0, 6))
	mock.ExpectQuery("SELECT .* FROM `comments` WHERE id = \\? AND video_id = \\? AND root_id = \\?.*FOR SHARE").WithArgs(uint64(22), uint64(7), uint64(11), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "video_id", "root_id", "user_id"}).AddRow(22, 7, 11, 8))
	mock.ExpectExec("INSERT INTO `comments`").WithArgs(uint64(7), uint64(9), uint64(11), uint64(22), uint64(8), commentRequestID, "reply", sqlmock.AnyArg(), nil).WillReturnResult(sqlmock.NewResult(33, 1))
	mock.ExpectCommit()
	c, err := CreateVideoComment(context.Background(), 7, 9, model.CreateCommentRequest{Content: "reply", ParentID: 22, RequestID: commentRequestID})
	if err != nil {
		t.Fatal(err)
	}
	if c.RootID != 11 || c.ParentID != 22 || c.ReplyToUserID != 8 {
		t.Fatalf("%+v", c)
	}
}

func TestCrossVideoAndDeletedThreadRejectReplies(t *testing.T) {
	for _, scenario := range []string{"cross video", "deleted root", "deleted parent"} {
		t.Run(scenario, func(t *testing.T) {
			mock := communityDB(t)
			mock.ExpectBegin()
			expectCommentVideo(mock, true, true)
			expectNoPriorComment(mock)
			parentRows := sqlmock.NewRows([]string{"id", "video_id", "root_id", "user_id"})
			if scenario != "cross video" {
				parentRows.AddRow(22, 7, 11, 8)
			}
			mock.ExpectQuery("SELECT .* FROM `comments` WHERE id = \\? AND video_id = \\?.*LIMIT \\?").WithArgs(uint64(22), uint64(7), 1).WillReturnRows(parentRows)
			if scenario != "cross video" {
				var deleted any
				if scenario == "deleted root" {
					deleted = time.Now()
				}
				mock.ExpectQuery("SELECT .* FROM `comments`.*root_id = 0.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"id", "video_id", "deleted_at"}).AddRow(11, 7, deleted))
				if scenario == "deleted parent" {
					mock.ExpectQuery("SELECT .* FROM `comments`.*FOR SHARE").WillReturnRows(sqlmock.NewRows([]string{"id", "video_id", "root_id", "deleted_at"}).AddRow(22, 7, 11, time.Now()))
				}
			}
			mock.ExpectRollback()
			_, err := CreateVideoComment(context.Background(), 7, 9, model.CreateCommentRequest{Content: "reply", ParentID: 22, RequestID: commentRequestID})
			want := ErrCommentClosed
			if scenario == "cross video" {
				want = ErrCommentTarget
			}
			if !errors.Is(err, want) {
				t.Fatal(err)
			}
		})
	}
}

func TestCommentRetryDoesNotInsertAgain(t *testing.T) {
	for _, content := range []string{"same", "different"} {
		t.Run(content, func(t *testing.T) {
			mock := communityDB(t)
			mock.ExpectBegin()
			expectCommentVideo(mock, true, true)
			mock.ExpectQuery("SELECT .* FROM `comments` WHERE user_id.*request_id.*").WillReturnRows(sqlmock.NewRows([]string{"id", "video_id", "user_id", "parent_id", "content"}).AddRow(5, 7, 9, 0, "same"))
			if content == "same" {
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			c, err := CreateVideoComment(context.Background(), 7, 9, model.CreateCommentRequest{Content: content, RequestID: commentRequestID})
			if content == "same" {
				if err != nil || c.ID != 5 {
					t.Fatalf("%+v %v", c, err)
				}
			} else if !errors.Is(err, ErrCommentRequest) {
				t.Fatal(err)
			}
		})
	}
}

func TestDeleteCommentClearsBodyPreservesTreeAndChecksOwner(t *testing.T) {
	for _, scenario := range []string{"delete", "retry", "wrong owner"} {
		t.Run(scenario, func(t *testing.T) {
			mock := communityDB(t)
			mock.ExpectBegin()
			rows := sqlmock.NewRows([]string{"id", "video_id", "user_id", "deleted_at"})
			var deleted any
			if scenario == "retry" {
				deleted = time.Now()
			}
			if scenario != "wrong owner" {
				rows.AddRow(5, 7, 9, deleted)
			}
			mock.ExpectQuery("SELECT .* FROM `comments` WHERE id = \\? AND video_id = \\? AND user_id = \\?.*FOR UPDATE").WithArgs(uint64(5), uint64(7), uint64(9), 1).WillReturnRows(rows)
			if scenario == "delete" {
				mock.ExpectExec("UPDATE `comments` SET `content`=\\?,`deleted_at`=\\? WHERE `id` = \\?").WithArgs("", sqlmock.AnyArg(), uint64(5)).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			if scenario == "wrong owner" {
				mock.ExpectRollback()
			} else {
				mock.ExpectCommit()
			}
			err := DeleteVideoComment(context.Background(), 7, 5, 9)
			if scenario == "wrong owner" {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCommentListRetainsTombstonesAndFiltersVideo(t *testing.T) {
	mock := communityDB(t)
	expectCommentVideo(mock, true, false)
	stamp := time.Now().Truncate(time.Microsecond)
	mock.ExpectQuery("SELECT .*CASE WHEN c.deleted_at IS NULL THEN c.content ELSE '' END AS content.*JOIN videos AS v.*v.publish_status = 'published'.*v.audit_status = 'approved'.*v.deleted_at IS NULL.*WHERE .*c.video_id = \\? AND c.root_id = \\?.*c.created_at < \\?.*c.id < \\?.*ORDER BY c.created_at DESC, c.id DESC LIMIT \\?").WithArgs(uint64(7), uint64(0), stamp, stamp, uint64(10), 21).
		WillReturnRows(sqlmock.NewRows([]string{"id", "content", "deleted_at", "reply_count"}).AddRow(5, "", stamp, 3))
	items, err := ListVideoComments(context.Background(), 7, 0, stamp, 10, 21)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Content != "" || items[0].DeletedAt == nil || items[0].ReplyCount != 3 {
		t.Fatal(items)
	}
}
