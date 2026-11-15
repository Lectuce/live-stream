package dao

import "gorm.io/gorm"

// Legacy comment report IDs remain in their old table; the target/user pair deduplicates imports.
func migrateReports(db *gorm.DB) error {
	return db.Exec(`INSERT INTO reports (target_type,target_id,user_id,reason,status,resolution,resolved_by,resolved_at,created_at)
 SELECT 'comment',CAST(comment_id AS CHAR),user_id,reason,status,resolution,resolved_by,resolved_at,created_at FROM comment_reports
 ON DUPLICATE KEY UPDATE id=reports.id`).Error
}
