package dao

import "gorm.io/gorm"

// Run after AutoMigrate and before HTTP starts. Retries never overwrite migrated targets.
// Stop old backend instances during this upgrade: old writers do not fill target_id.
func migrateCommentTargets(db *gorm.DB) error {
	return db.Exec("UPDATE comments SET target_id = video_id WHERE target_type = 'video' AND target_id = 0 AND video_id > 0").Error
}
