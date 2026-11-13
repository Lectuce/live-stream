package repo

import (
	"context"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"live-stream/internal/dao"
	"live-stream/internal/model"
	"strings"
	"testing"
)

// SQL-scope regression test; no running database is required.
func TestVideoQueriesExcludeSoftDeletedRecords(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{DSN: "test:test@tcp(127.0.0.1:1)/test", SkipInitializeWithVersion: true}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	previous := dao.SC
	dao.SC = &model.StorageContainer{Db: db}
	t.Cleanup(func() { dao.SC = previous })
	var sql string
	capture := func(tx *gorm.DB) { sql = tx.Statement.SQL.String() }
	if err := db.Callback().Query().After("gorm:query").Register("test:capture", capture); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().After("gorm:update").Register("test:capture", capture); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Delete().After("gorm:delete").Register("test:capture", capture); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	cases := map[string]func() error{
		"public detail": func() error { _, err := SelectVideo(ctx, 1); return err },
		"owner detail":  func() error { _, err := SelectOwnerVideo(ctx, 1, 2); return err },
		"owner list":    func() error { _, err := SelectOwnerVideos(ctx, 2, 0, 20); return err },
		"public list":   func() error { _, err := SelectPublishedVideos(ctx, 0, 20); return err },
		"publish":       func() error { _, err := PublishVideo(ctx, 1, 2); return err },
		"attach upload": func() error { _, err := AttachVideoMedia(ctx, 1, 2, "x.mp4", "/media", 10, "approved"); return err },
		"delete":        func() error { _, err := SoftDeleteOwnerVideo(ctx, 1, 2); return err },
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			sql = ""
			if err := run(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(sql, "`videos`.`deleted_at` IS NULL") {
				t.Fatalf("soft-delete scope missing: %s", sql)
			}
			if name == "delete" && !strings.HasPrefix(sql, "UPDATE ") {
				t.Fatalf("expected soft delete: %s", sql)
			}
		})
	}
}
