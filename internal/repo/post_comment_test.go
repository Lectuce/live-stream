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

func expectCommentPost(mock sqlmock.Sqlmock, exists, lock bool) {
	query := "SELECT `post_id` FROM `posts` WHERE .*post_id = \\? AND status = 'published'.*`posts`.`deleted_at` IS NULL.*LIMIT \\?"
	if lock {
		query += " FOR SHARE"
	}
	rows := sqlmock.NewRows([]string{"post_id"})
	if exists {
		rows.AddRow(7)
	}
	mock.ExpectQuery(query).WithArgs(uint64(7), 1).WillReturnRows(rows)
}

func TestPostCommentsRequirePublishedUndeletedTarget(t *testing.T) {
	for _, write := range []bool{false, true} {
		t.Run(map[bool]string{true: "create", false: "read"}[write], func(t *testing.T) {
			mock := communityDB(t)
			if write {
				mock.ExpectBegin()
			}
			expectCommentPost(mock, false, write)
			var err error
			if write {
				mock.ExpectRollback()
				_, err = CreateComment(context.Background(), "post", 7, 9, model.CreateCommentRequest{Content: "hello", RequestID: commentRequestID})
			} else {
				_, err = ListComments(context.Background(), "post", 7, 0, time.Time{}, 0, 21)
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatal(err)
			}
		})
	}
}

func TestCreatePostRootAndReply(t *testing.T) {
	for _, parentID := range []uint64{0, 22} {
		t.Run(map[uint64]string{0: "root", 22: "reply to reply"}[parentID], func(t *testing.T) {
			mock := communityDB(t)
			mock.ExpectBegin()
			expectCommentPost(mock, true, true)
			expectNoPriorComment(mock)
			var rootID, replyTo uint64
			if parentID != 0 {
				rootID, replyTo = 11, 8
				mock.ExpectQuery("SELECT .* FROM `comments` WHERE id = \\? AND target_type = \\? AND target_id = \\?.*LIMIT \\?").WithArgs(parentID, "post", uint64(7), 1).
					WillReturnRows(sqlmock.NewRows([]string{"id", "root_id", "user_id"}).AddRow(22, 11, 8))
				mock.ExpectQuery("SELECT .* FROM `comments` WHERE id = \\? AND target_type = \\? AND target_id = \\? AND root_id = 0.*FOR UPDATE").WithArgs(rootID, "post", uint64(7), 1).
					WillReturnRows(sqlmock.NewRows([]string{"id", "root_id", "user_id"}).AddRow(11, 0, 6))
				mock.ExpectQuery("SELECT .* FROM `comments` WHERE id = \\? AND target_type = \\? AND target_id = \\? AND root_id = \\?.*FOR SHARE").WithArgs(parentID, "post", uint64(7), rootID, 1).
					WillReturnRows(sqlmock.NewRows([]string{"id", "root_id", "user_id"}).AddRow(22, 11, 8))
			}
			mock.ExpectExec("INSERT INTO `comments`").WithArgs(uint64(0), "post", uint64(7), uint64(9), rootID, parentID, replyTo, commentRequestID, "hello", sqlmock.AnyArg(), nil).WillReturnResult(sqlmock.NewResult(33, 1))
			mock.ExpectCommit()
			c, err := CreateComment(context.Background(), "post", 7, 9, model.CreateCommentRequest{Content: "hello", ParentID: parentID, RequestID: commentRequestID})
			if err != nil || c.TargetType != "post" || c.TargetID != 7 || c.VideoID != 0 || c.RootID != rootID || c.ReplyToUserID != replyTo {
				t.Fatalf("%+v %v", c, err)
			}
		})
	}
}

func TestPostReplyRejectsParentFromVideoWithSameID(t *testing.T) {
	mock := communityDB(t)
	mock.ExpectBegin()
	expectCommentPost(mock, true, true)
	expectNoPriorComment(mock)
	// Comment 22 belongs to video 7. Target type must be part of the lookup, not just ID 7.
	mock.ExpectQuery("SELECT .* FROM `comments` WHERE id = \\? AND target_type = \\? AND target_id = \\?.*LIMIT \\?").WithArgs(uint64(22), "post", uint64(7), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()
	_, err := CreateComment(context.Background(), "post", 7, 9, model.CreateCommentRequest{Content: "hello", ParentID: 22, RequestID: commentRequestID})
	if !errors.Is(err, ErrCommentTarget) {
		t.Fatal(err)
	}
}

func TestPostCommentIdempotencyIncludesTargetType(t *testing.T) {
	for _, originalType := range []string{"post", "video"} {
		t.Run(originalType, func(t *testing.T) {
			mock := communityDB(t)
			mock.ExpectBegin()
			expectCommentPost(mock, true, true)
			mock.ExpectQuery("SELECT .* FROM `comments` WHERE user_id.*request_id.*").WithArgs(uint64(9), commentRequestID, 1).
				WillReturnRows(sqlmock.NewRows([]string{"id", "target_type", "target_id", "parent_id", "content"}).AddRow(5, originalType, 7, 0, "hello"))
			if originalType == "post" {
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			c, err := CreateComment(context.Background(), "post", 7, 9, model.CreateCommentRequest{Content: "hello", RequestID: commentRequestID})
			if originalType == "post" {
				if err != nil || c.ID != 5 {
					t.Fatalf("%+v %v", c, err)
				}
			} else if !errors.Is(err, ErrCommentRequest) {
				t.Fatal(err)
			}
		})
	}
}

func TestPostCommentListScopesRepliesAndCounts(t *testing.T) {
	mock := communityDB(t)
	expectCommentPost(mock, true, false)
	mock.ExpectQuery("SELECT `id` FROM `comments` WHERE id = \\? AND target_type = \\? AND target_id = \\? AND root_id = 0.*LIMIT \\?").WithArgs(uint64(11), "post", uint64(7), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(11))
	mock.ExpectQuery("SELECT .*r.target_type = c.target_type AND r.target_id = c.target_id AND r.root_id = c.id.*JOIN posts AS p ON p.post_id = c.target_id AND p.status = 'published' AND p.deleted_at IS NULL.*WHERE c.target_type = \\? AND c.target_id = \\? AND c.root_id = \\?.*ORDER BY c.created_at DESC, c.id DESC LIMIT \\?").WithArgs("post", uint64(7), uint64(11), 21).
		WillReturnRows(sqlmock.NewRows([]string{"id", "target_type", "target_id", "root_id", "content"}).AddRow(22, "post", 7, 11, "reply"))
	items, err := ListComments(context.Background(), "post", 7, 11, time.Time{}, 0, 21)
	if err != nil || len(items) != 1 || items[0].TargetType != "post" {
		t.Fatalf("%+v %v", items, err)
	}
}

func TestPostRepliesRejectForeignRoot(t *testing.T) {
	mock := communityDB(t)
	expectCommentPost(mock, true, false)
	mock.ExpectQuery("SELECT `id` FROM `comments` WHERE id = \\? AND target_type = \\? AND target_id = \\? AND root_id = 0.*LIMIT \\?").WithArgs(uint64(11), "post", uint64(7), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	_, err := ListComments(context.Background(), "post", 7, 11, time.Time{}, 0, 21)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal(err)
	}
}

func TestPostCommentDeleteChecksTypeTargetAndOwner(t *testing.T) {
	for _, found := range []bool{true, false} {
		t.Run(map[bool]string{true: "own", false: "foreign target or owner"}[found], func(t *testing.T) {
			mock := communityDB(t)
			mock.ExpectBegin()
			rows := sqlmock.NewRows([]string{"id"})
			if found {
				rows.AddRow(22)
			}
			mock.ExpectQuery("SELECT .* FROM `comments` WHERE id = \\? AND target_type = \\? AND target_id = \\? AND user_id = \\?.*FOR UPDATE").WithArgs(uint64(22), "post", uint64(7), uint64(9), 1).WillReturnRows(rows)
			if found {
				mock.ExpectExec("UPDATE `comments` SET `content`=\\?,`deleted_at`=\\? WHERE `id` = \\?").WithArgs("", sqlmock.AnyArg(), uint64(22)).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			err := DeleteComment(context.Background(), "post", 7, 22, 9)
			if (found && err != nil) || (!found && !errors.Is(err, gorm.ErrRecordNotFound)) {
				t.Fatal(err)
			}
		})
	}
}
