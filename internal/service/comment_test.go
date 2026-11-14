package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"live-stream/internal/dao"
	"live-stream/internal/model"
)

func TestNormalizeComment(t *testing.T) {
	valid := model.CreateCommentRequest{Content: " 你好 \n", RequestID: "ae9334e0-e17b-4c58-8a90-95127fbc960e"}
	got, err := normalizeComment(valid)
	if err != nil || got.Content != "你好" {
		t.Fatal(got, err)
	}
	for _, content := range []string{"", " \n ", strings.Repeat("字", 2001)} {
		req := valid
		req.Content = content
		if _, err := normalizeComment(req); err == nil {
			t.Fatal("invalid content accepted")
		}
	}
	valid.RequestID = "invalid"
	if _, err := normalizeComment(valid); err == nil {
		t.Fatal("invalid request ID")
	}
}

func TestCommentCursorScope(t *testing.T) {
	stamp := time.Date(2026, 11, 14, 1, 2, 3, 123456000, time.UTC)
	encode := func(c commentCursor) string {
		data, _ := json.Marshal(c)
		return base64.RawURLEncoding.EncodeToString(data)
	}
	valid := commentCursor{Version: 1, VideoID: 7, RootID: 11, ID: 22, Time: stamp}
	if _, err := decodeCommentCursor(encode(valid), 7, 11); err != nil {
		t.Fatal(err)
	}
	for _, scope := range [][2]uint64{{8, 11}, {7, 12}, {7, 0}} {
		if _, err := decodeCommentCursor(encode(valid), scope[0], scope[1]); err == nil {
			t.Fatal("cross-scope cursor accepted")
		}
	}
	for _, value := range []string{"invalid", strings.Repeat("x", 1025), encode(commentCursor{Version: 1, VideoID: 7, RootID: 11, ID: 0, Time: stamp})} {
		if _, err := decodeCommentCursor(value, 7, 11); err == nil {
			t.Fatal("invalid cursor accepted")
		}
	}
}

func TestCommentPaginationCursorUsesLastReturnedRow(t *testing.T) {
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
	stamp := time.Now().UTC().Truncate(time.Microsecond)
	mock.ExpectQuery("SELECT `video_id` FROM `videos`").WillReturnRows(sqlmock.NewRows([]string{"video_id"}).AddRow(7))
	mock.ExpectQuery("SELECT .*FROM comments AS c.*LIMIT \\?").WithArgs(uint64(7), uint64(0), 3).WillReturnRows(sqlmock.NewRows([]string{"id", "video_id", "created_at"}).AddRow(30, 7, stamp).AddRow(20, 7, stamp).AddRow(10, 7, stamp))
	page, err := ListVideoComments(context.Background(), 7, 0, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || !page.HasMore {
		t.Fatal(page)
	}
	cursor, err := decodeCommentCursor(page.NextCursor, 7, 0)
	if err != nil || cursor.ID != 20 || !cursor.Time.Equal(stamp) {
		t.Fatal(cursor, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
