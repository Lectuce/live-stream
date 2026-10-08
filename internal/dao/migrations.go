package dao

import (
	"fmt"
	"live-stream/internal/model"

	"gorm.io/gorm"
)

// migrateDanmaSequences 特意设计为感知现有数据。AutoMigrate 可以添加列，
// 但无法在创建唯一索引前为已有记录生成稳定的房间内顺序。
func migrateDanmaSequences(db *gorm.DB) error {
	var missingSequences int64
	if err := db.Raw(
		"SELECT COUNT(*) FROM danmas WHERE room_seq IS NULL OR room_seq = 0",
	).Scan(&missingSequences).Error; err != nil {
		return fmt.Errorf("count danmas without room sequence: %w", err)
	}

	const backfill = `
UPDATE danmas AS d
JOIN (
    SELECT id, ROW_NUMBER() OVER (PARTITION BY room_id ORDER BY id) AS room_seq
    FROM danmas
) AS ranked ON ranked.id = d.id
SET d.room_seq = ranked.room_seq
WHERE d.room_seq IS NULL OR d.room_seq = 0`
	if missingSequences > 0 {
		if err := db.Exec(backfill).Error; err != nil {
			return fmt.Errorf("backfill danmas.room_seq: %w", err)
		}
	}

	if !db.Migrator().HasIndex(&model.Danma{}, "idx_danmas_room_seq") {
		if err := db.Exec(
			"CREATE UNIQUE INDEX idx_danmas_room_seq ON danmas (room_id, room_seq)",
		).Error; err != nil {
			return fmt.Errorf("create room sequence index: %w", err)
		}
	}
	return nil
}
