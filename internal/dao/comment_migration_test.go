package dao

import (
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestCommentTargetBackfillOnlyTouchesUnmigratedVideos(t *testing.T) {
	conn, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: conn, SkipInitializeWithVersion: true}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	query := regexp.QuoteMeta("UPDATE comments SET target_id = video_id WHERE target_type = 'video' AND target_id = 0 AND video_id > 0")
	// A second startup is a no-op. Never rewrite post targets or reset deleted comment bodies.
	for _, affected := range []int64{3, 0} {
		mock.ExpectExec(query).WillReturnResult(sqlmock.NewResult(0, affected))
		if err := migrateCommentTargets(db); err != nil {
			t.Fatal(err)
		}
	}
	failure := errors.New("database unavailable")
	mock.ExpectExec(query).WillReturnError(failure)
	if err := migrateCommentTargets(db); !errors.Is(err, failure) {
		t.Fatal("startup must fail if the backfill fails", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
