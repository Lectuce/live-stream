package repo

import (
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"live-stream/internal/dao"
	"live-stream/internal/model"
	"testing"
)

func TestReviewTransactionAtomicity(t *testing.T) {
	for _, scenario := range []string{"success", "stale", "log failure"} {
		t.Run(scenario, func(t *testing.T) {
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
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT .*videos.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"video_id", "content_version", "audit_status", "process_status", "publish_status", "media_file"}).AddRow(1, 3, "pending", "ready", "draft", "file.mp4"))
			guardErr := errors.New("stale")
			if scenario == "stale" {
				mock.ExpectRollback()
			} else {
				mock.ExpectExec("UPDATE .*videos.*").WillReturnResult(sqlmock.NewResult(0, 1))
				if scenario == "log failure" {
					mock.ExpectExec("INSERT INTO .*video_reviews.*").WillReturnError(errors.New("log write failed"))
					mock.ExpectRollback()
				} else {
					mock.ExpectExec("INSERT INTO .*video_reviews.*").WillReturnResult(sqlmock.NewResult(1, 1))
					mock.ExpectCommit()
				}
			}
			err = ReviewVideoTransaction(context.Background(), 1, 42, model.ReviewVideoRequest{ContentVersion: 3, Decision: "approved"}, func(v *model.Video) error {
				if scenario == "stale" {
					return guardErr
				}
				return nil
			})
			if (scenario == "success") != (err == nil) {
				t.Fatalf("scenario=%s err=%v", scenario, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
