package repo

import (
	"context"
	"sync"
	"testing"

	"live-stream/internal/dao"
	"live-stream/internal/model"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestFollowPairUniqueIndex(t *testing.T) {
	s, err := schema.Parse(&model.UserFollow{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range s.ParseIndexes() {
		if index.Name == "idx_user_follow_pair" && index.Class == "UNIQUE" && len(index.Fields) == 2 &&
			index.Fields[0].DBName == "follower_id" && index.Fields[1].DBName == "followee_id" {
			return
		}
	}
	t.Fatal("missing unique (follower_id, followee_id) index")
}

func TestAuthorVideosEnforceVisibilityAndKeyset(t *testing.T) {
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
	mock.ExpectQuery("SELECT .* FROM `videos` WHERE .*owner_id = \\? AND publish_status = \\? AND process_status = \\? AND audit_status = \\?.*video_id < \\?.*`videos`.`deleted_at` IS NULL ORDER BY video_id DESC LIMIT \\?").
		WithArgs(uint64(2), "published", "ready", "approved", uint64(99), 21).
		WillReturnRows(sqlmock.NewRows([]string{"video_id", "owner_id"}).AddRow(98, 2))
	videos, err := SelectPublicAuthorVideos(context.Background(), 2, 99, 21)
	if err != nil {
		t.Fatal(err)
	}
	if len(videos) != 1 || videos[0].VideoID != 98 {
		t.Fatal(videos)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
