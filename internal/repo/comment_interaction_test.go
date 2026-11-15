package repo

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"
	"live-stream/internal/model"
)

func interactionRow(status string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "target_type", "target_id", "user_id", "root_id", "version", "content", "audit_status"}).AddRow(5, "video", 7, 9, 0, 3, "original", status)
}

func expectInteractionLock(mock sqlmock.Sqlmock, status string, public bool) {
	mock.ExpectQuery("SELECT .* FROM `comments` WHERE `comments`.`id` = \\?.*LIMIT \\?").WithArgs(uint64(5), 1).WillReturnRows(interactionRow(status))
	if public {
		expectCommentVideo(mock, true, true)
	}
	mock.ExpectQuery("SELECT .* FROM `comments` WHERE `comments`.`id` = \\?.*FOR UPDATE").WithArgs(uint64(5), 1).WillReturnRows(interactionRow(status))
}

func TestCommentEditChecksOwnerVersionAndWritesAuditAtomically(t *testing.T) {
	for _, scenario := range []string{"success", "other author", "stale", "audit failure"} {
		t.Run(scenario, func(t *testing.T) {
			mock := communityDB(t)
			mock.ExpectBegin()
			expectInteractionLock(mock, "approved", false)
			actor, version := uint64(9), uint64(3)
			if scenario == "other author" {
				actor = 8
			}
			if scenario == "stale" {
				version = 2
			}
			if scenario == "success" || scenario == "audit failure" {
				e := mock.ExpectExec("INSERT INTO `comment_reviews`").WithArgs(uint64(5), actor, uint64(3), "edited", "", sqlmock.AnyArg(), sqlmock.AnyArg())
				if scenario == "audit failure" {
					e.WillReturnError(errors.New("log unavailable"))
				} else {
					e.WillReturnResult(sqlmock.NewResult(1, 1))
					mock.ExpectExec("UPDATE `comments` SET `audit_reason`=\\?,`audit_status`=\\?,`content`=\\?,`version`=version \\+ 1 WHERE `id` = \\?").WithArgs("", "pending", "edited", uint64(5)).WillReturnResult(sqlmock.NewResult(0, 1))
				}
			}
			if scenario == "success" {
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			err := EditComment(context.Background(), 5, actor, model.EditCommentRequest{Version: version, Content: "edited"})
			switch scenario {
			case "success":
				if err != nil {
					t.Fatal(err)
				}
			case "other author":
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					t.Fatal(err)
				}
			case "stale":
				if !errors.Is(err, ErrCommentConflict) {
					t.Fatal(err)
				}
			default:
				if err == nil {
					t.Fatal("audit failure committed")
				}
			}
		})
	}
}

func TestCommentReviewRejectsOldVersionAndRollsBackLogFailure(t *testing.T) {
	for _, scenario := range []string{"success", "stale", "audit failure"} {
		t.Run(scenario, func(t *testing.T) {
			mock := communityDB(t)
			mock.ExpectBegin()
			expectInteractionLock(mock, "pending", false)
			version := uint64(3)
			if scenario == "stale" {
				version = 2
			} else {
				e := mock.ExpectExec("INSERT INTO `comment_reviews`").WithArgs(uint64(5), uint64(42), uint64(3), "approved", "", sqlmock.AnyArg(), sqlmock.AnyArg())
				if scenario == "audit failure" {
					e.WillReturnError(errors.New("audit write failed"))
				} else {
					e.WillReturnResult(sqlmock.NewResult(1, 1))
					mock.ExpectExec("UPDATE `comments` SET `audit_reason`=\\?,`audit_status`=\\?,`version`=version \\+ 1 WHERE `id` = \\?").WithArgs("", "approved", uint64(5)).WillReturnResult(sqlmock.NewResult(0, 1))
				}
			}
			if scenario == "success" {
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			err := ReviewComment(context.Background(), 5, 42, model.ReviewCommentRequest{Version: version, Decision: "approved"})
			if scenario == "success" && err != nil {
				t.Fatal(err)
			}
			if scenario != "success" && err == nil {
				t.Fatal("invalid review committed")
			}
		})
	}
}

func TestCommentLikesAreDerivedFromUniqueRelations(t *testing.T) {
	for _, scenario := range []string{"first like", "duplicate like", "cancel", "duplicate cancel"} {
		t.Run(scenario, func(t *testing.T) {
			mock := communityDB(t)
			mock.ExpectBegin()
			exists := int64(0)
			if scenario == "duplicate like" || scenario == "cancel" {
				exists = 1
			}
			rows := func() *sqlmock.Rows {
				return sqlmock.NewRows([]string{"id", "target_type", "target_id", "audit_status", "like_count"}).AddRow(5, "video", 7, "approved", exists)
			}
			mock.ExpectQuery("SELECT .* FROM `comments`.*LIMIT").WithArgs(uint64(5), 1).WillReturnRows(rows())
			expectCommentVideo(mock, true, true)
			mock.ExpectQuery("SELECT .* FROM `comments`.*FOR UPDATE").WithArgs(uint64(5), 1).WillReturnRows(rows())
			mock.ExpectQuery("SELECT count.* FROM `comment_likes` WHERE comment_id = \\? AND user_id = \\?").WithArgs(uint64(5), uint64(9)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(exists))
			liked := scenario == "first like" || scenario == "duplicate like"
			count := int64(0)
			if liked {
				count = 1
			}
			if scenario == "first like" {
				mock.ExpectExec("INSERT INTO `comment_likes`").WithArgs(uint64(5), uint64(9), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			if scenario == "cancel" {
				mock.ExpectExec("DELETE FROM `comment_likes` WHERE comment_id = \\? AND user_id = \\?").WithArgs(uint64(5), uint64(9)).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			if scenario == "first like" || scenario == "cancel" {
				mock.ExpectExec("UPDATE `comments` SET `like_count`=\\? WHERE `id` = \\?").WithArgs(count, uint64(5)).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			mock.ExpectCommit()
			got, err := SetCommentLike(context.Background(), 5, 9, liked)
			if err != nil || got != count {
				t.Fatal(got, err)
			}
		})
	}
}

func TestPendingCommentsCannotBeLikedOrReported(t *testing.T) {
	for _, action := range []string{"like", "report"} {
		t.Run(action, func(t *testing.T) {
			mock := communityDB(t)
			mock.ExpectBegin()
			expectInteractionLock(mock, "pending", true)
			mock.ExpectRollback()
			var err error
			if action == "like" {
				_, err = SetCommentLike(context.Background(), 5, 9, true)
			} else {
				err = ReportComment(context.Background(), 5, 9, "spam")
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatal(err)
			}
		})
	}
}

func TestHiddenRootBlocksChildInteractions(t *testing.T) {
	mock := communityDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `comments` WHERE `comments`.`id` = \\?.*LIMIT \\?").WithArgs(uint64(5), 1).WillReturnRows(sqlmock.NewRows([]string{"id", "target_type", "target_id", "root_id"}).AddRow(5, "video", 7, 2))
	expectCommentVideo(mock, true, true)
	mock.ExpectQuery("SELECT .* FROM `comments` WHERE id = \\? AND target_type = \\? AND target_id = \\? AND root_id = 0.*FOR UPDATE").WithArgs(uint64(2), "video", uint64(7), 1).WillReturnRows(sqlmock.NewRows([]string{"id", "audit_status"}).AddRow(2, "hidden"))
	mock.ExpectRollback()
	_, err := SetCommentLike(context.Background(), 5, 9, true)
	if !errors.Is(err, ErrCommentClosed) {
		t.Fatal(err)
	}
}

func TestCommentReportIsUniqueAndResolutionIsAudited(t *testing.T) {
	mock := communityDB(t)
	mock.ExpectBegin()
	expectInteractionLock(mock, "approved", true)
	mock.ExpectExec("INSERT INTO `comment_reports`.*ON DUPLICATE KEY UPDATE").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	if err := ReportComment(context.Background(), 5, 9, "spam"); err != nil {
		t.Fatal(err)
	}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `comment_reports`.*FOR UPDATE").WithArgs(uint64(8), 1).WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow(8, "pending"))
	mock.ExpectExec("UPDATE `comment_reports` SET `resolution`=\\?,`resolved_at`=\\?,`resolved_by`=\\?,`status`=\\? WHERE `id` = \\?").WithArgs("checked", sqlmock.AnyArg(), uint64(42), "dismissed", uint64(8)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := ResolveCommentReport(context.Background(), 8, 42, "dismissed", "checked"); err != nil {
		t.Fatal(err)
	}
}
