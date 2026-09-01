package dao

import (
	"fmt"
	"live-stream/internal/model"

	"gorm.io/gorm"
)

// migrateDanmaSequences is intentionally data-aware. AutoMigrate can add a
// column, but it cannot give existing rows a stable per-room order before a
// unique index is created.
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

	const seedCounters = `
INSERT INTO room_message_sequences (room_id, last_seq, updated_at)
SELECT room_id, MAX(room_seq), NOW(3)
FROM danmas
GROUP BY room_id
ON DUPLICATE KEY UPDATE
    last_seq = GREATEST(last_seq, VALUES(last_seq)),
    updated_at = VALUES(updated_at)`
	if err := db.Exec(seedCounters).Error; err != nil {
		return fmt.Errorf("seed room message sequences: %w", err)
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
