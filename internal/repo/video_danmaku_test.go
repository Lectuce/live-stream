package repo

import (
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"
	"live-stream/internal/model"
	"testing"
)

func TestVideoDanmakuCreationChecksDurationAndIdempotency(t *testing.T) {
	for _, scenario := range []string{"new", "retry", "conflict", "out of range", "private"} {
		t.Run(scenario, func(t *testing.T) {
			mock := communityDB(t)
			mock.ExpectBegin()
			video := sqlmock.NewRows([]string{"video_id", "duration_ms"})
			if scenario != "private" {
				video.AddRow(7, 30000)
			}
			mock.ExpectQuery("SELECT .* FROM `videos`.*publish_status='published'.*audit_status='approved'.*FOR UPDATE").WithArgs(uint64(7), 1).WillReturnRows(video)
			item := model.VideoDanmaku{VideoID: 7, UserID: 9, RequestID: "req", PositionMS: 1000, Content: "hello", Color: "#ffffff"}
			if scenario == "out of range" {
				item.PositionMS = 30000
			}
			if scenario != "private" && scenario != "out of range" {
				rows := sqlmock.NewRows([]string{"id", "video_id", "position_ms", "content", "color"})
				if scenario == "retry" || scenario == "conflict" {
					rows.AddRow(4, 7, 1000, "hello", "#ffffff")
				}
				if scenario == "conflict" {
					item.Content = "changed"
				}
				mock.ExpectQuery("SELECT .* FROM `video_danmakus` WHERE user_id = .*request_id = .*LIMIT").WithArgs(uint64(9), "req", 1).WillReturnRows(rows)
				if scenario == "new" {
					mock.ExpectExec("INSERT INTO `video_danmakus`").WillReturnResult(sqlmock.NewResult(4, 1))
				}
			}
			good := scenario == "new" || scenario == "retry"
			if good {
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			err := CreateVideoDanmaku(context.Background(), &item)
			if good && (err != nil || item.ID != 4) {
				t.Fatal(item, err)
			}
			if !good && err == nil {
				t.Fatal("invalid write accepted")
			}
			if scenario == "private" && !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatal(err)
			}
		})
	}
}
func TestVideoDanmakuDeleteCannotCrossOwnerOrVideo(t *testing.T) {
	mock := communityDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `video_danmakus` WHERE id = .*video_id = .*user_id = .*FOR UPDATE").WithArgs(uint64(4), uint64(7), uint64(9), 1).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()
	if err := DeleteVideoDanmaku(context.Background(), 7, 4, 9); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal(err)
	}
}
