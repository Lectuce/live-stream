package repo

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"live-stream/internal/dao"
	"live-stream/internal/model"
)

type ReportTarget struct {
	Preview string `json:"preview"`
	Version uint64 `json:"version,string"`
	URL     string `json:"url"`
}
type ReportView struct {
	model.Report
	ReportTarget
}

func reportTarget(db *gorm.DB, kind, ref string, public, lock bool) (ReportTarget, error) {
	var target ReportTarget
	if lock && kind != "comment" {
		db = db.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	id, _ := strconv.ParseUint(ref, 10, 64)
	switch kind {
	case "video":
		var v model.Video
		q := db.Where("video_id = ?", id)
		if public {
			q = q.Where("publish_status='published' AND audit_status='approved' AND process_status='ready'")
		}
		if err := q.First(&v).Error; err != nil {
			return target, err
		}
		target = ReportTarget{v.Title + "\n" + v.Description, v.ContentVersion, fmt.Sprintf("/video/%d", id)}
	case "post":
		var p model.Post
		q := db.Where("post_id = ?", id)
		if public {
			q = q.Where("status='published'")
		}
		if err := q.First(&p).Error; err != nil {
			return target, err
		}
		target = ReportTarget{p.Content, p.Version, fmt.Sprintf("/posts/%d", id)}
	case "comment":
		var c model.Comment
		if lock {
			locked, err := lockComment(db, id, false)
			if err != nil {
				return target, err
			}
			c = *locked
		} else if err := db.First(&c, id).Error; err != nil {
			return target, err
		}
		if c.DeletedAt != nil {
			return target, gorm.ErrRecordNotFound
		}
		if public {
			var view model.CommentView
			r := publicCommentQuery(db, c.TargetType, c.TargetID).Where("c.id = ? AND c.deleted_at IS NULL", id).Limit(1).Scan(&view)
			if r.Error != nil {
				return target, r.Error
			}
			if r.RowsAffected == 0 {
				return target, gorm.ErrRecordNotFound
			}
		}
		path := "posts"
		if c.TargetType == "video" {
			path = "video"
		}
		target = ReportTarget{c.Content, c.Version, fmt.Sprintf("/%s/%d", path, c.TargetID)}
	case "video_danmaku":
		var d model.VideoDanmaku
		q := db.Where("id = ? AND deleted_at IS NULL", id)
		if public {
			q = q.Where("hidden = false")
		}
		if err := q.First(&d).Error; err != nil {
			return target, err
		}
		if public {
			if err := publicCommentTarget(db.Session(&gorm.Session{NewDB: true}), "video", d.VideoID, false); err != nil {
				return target, err
			}
		}
		target = ReportTarget{d.Content, d.Version, fmt.Sprintf("/video/%d", d.VideoID)}
	case "danmaku":
		var d model.Danma
		q := db.Where("message_id = ?", ref)
		if public {
			q = q.Where("hidden_at IS NULL")
		}
		if err := q.First(&d).Error; err != nil {
			return target, err
		}
		target = ReportTarget{d.Content, d.ModerationVersion, fmt.Sprintf("/rooms/%d", d.RoomID)}
	default:
		return target, ErrCommentTarget
	}
	return target, nil
}

func SubmitReport(ctx context.Context, kind, ref string, actor uint64, reason string) error {
	db := dao.SC.Db.WithContext(ctx)
	if _, err := reportTarget(db, kind, ref, true, false); err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.Report{TargetType: kind, TargetID: ref, UserID: actor, Reason: reason, Status: "pending"}).Error
}

func ListReports(ctx context.Context, status, kind string, before uint64, limit int) ([]ReportView, error) {
	db := dao.SC.Db.WithContext(ctx)
	rows := make([]ReportView, 0)
	q := db.Model(&model.Report{}).Select("reports.*").Where("status = ?", status)
	if kind != "" {
		q = q.Where("target_type = ?", kind)
	}
	if before > 0 {
		q = q.Where("id < ?", before)
	}
	if err := q.Order("id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		target, err := reportTarget(db, rows[i].TargetType, rows[i].TargetID, false, false)
		if err == gorm.ErrRecordNotFound || err == ErrCommentClosed {
			continue
		}
		if err != nil {
			return nil, err
		}
		rows[i].ReportTarget = target
	}
	return rows, nil
}

func ResolveReport(ctx context.Context, id, actor, version uint64, action, reason string) error {
	return dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var report model.Report
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&report, id).Error; err != nil {
			return err
		}
		status := "resolved"
		if action == "dismissed" {
			status = "dismissed"
		}
		if action == "hidden" || action == "restored" {
			target, err := reportTarget(tx, report.TargetType, report.TargetID, false, true)
			if err != nil {
				return err
			}
			if target.Version != version {
				return ErrCommentConflict
			}
			if err := moderateReportTarget(tx, &report, actor, version, action == "hidden", reason); err != nil {
				return err
			}
		} else if report.Status != "pending" {
			if report.Status == status && report.Resolution == reason && report.ResolvedBy == actor {
				return nil
			}
			return ErrCommentConflict
		}
		if err := tx.Create(&model.ReportAction{ReportID: id, ActorID: actor, Action: action, Reason: reason}).Error; err != nil {
			return err
		}
		return tx.Model(&report).Updates(map[string]any{"status": status, "resolution": reason, "resolved_by": actor, "resolved_at": time.Now().UTC()}).Error
	})
}

func moderateReportTarget(tx *gorm.DB, r *model.Report, actor, version uint64, hide bool, reason string) error {
	id, _ := strconv.ParseUint(r.TargetID, 10, 64)
	switch r.TargetType {
	case "video":
		status := "pending"
		if hide {
			status = "rejected"
		}
		if err := tx.Model(&model.Video{}).Where("video_id = ?", id).Updates(map[string]any{"audit_status": status, "publish_status": "draft", "audit_reason": reason, "content_version": gorm.Expr("content_version + 1")}).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.VideoReview{VideoID: id, ContentVersion: version, ReviewerID: actor, ReviewerType: "human", Decision: status, Reason: reason}).Error; err != nil {
			return err
		}
		var v model.Video
		if err := tx.Select("owner_id").Where("video_id = ?", id).First(&v).Error; err != nil {
			return err
		}
		return QueueNotification(tx, fmt.Sprintf("report.video:%d:%d", id, version), "video.review", "video", id, actor, v.OwnerID)
	case "post":
		status := "draft"
		if hide {
			status = "hidden"
		}
		return tx.Model(&model.Post{}).Where("post_id = ?", id).Updates(map[string]any{"status": status, "version": gorm.Expr("version + 1")}).Error
	case "comment":
		var c model.Comment
		if err := tx.First(&c, id).Error; err != nil {
			return err
		}
		status := "approved"
		if hide {
			status = "hidden"
		}
		if err := recordCommentReview(tx, &c, actor, status, reason); err != nil {
			return err
		}
		if err := tx.Model(&c).Updates(map[string]any{"audit_status": status, "audit_reason": reason, "version": gorm.Expr("version + 1")}).Error; err != nil {
			return err
		}
		return QueueNotification(tx, fmt.Sprintf("report.comment:%d:%d", id, version), "comment.review", "comment", id, actor, c.UserID)
	case "danmaku":
		var hidden *time.Time
		if hide {
			now := time.Now().UTC()
			hidden = &now
		}
		return tx.Model(&model.Danma{}).Where("message_id = ?", r.TargetID).Updates(map[string]any{"hidden_at": hidden, "moderation_version": gorm.Expr("moderation_version + 1")}).Error
	case "video_danmaku":
		return tx.Model(&model.VideoDanmaku{}).Where("id = ? AND deleted_at IS NULL", id).Updates(map[string]any{"hidden": hide, "version": gorm.Expr("version + 1")}).Error
	}
	return ErrCommentTarget
}
func ReportHistory(ctx context.Context, id, before uint64, limit int) ([]model.ReportAction, error) {
	rows := make([]model.ReportAction, 0)
	q := dao.SC.Db.WithContext(ctx).Where("report_id = ?", id)
	if before > 0 {
		q = q.Where("id < ?", before)
	}
	err := q.Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

// Redact cached and persisted replay pages uniformly; retain sequence positions so cursors still advance.
func RedactHiddenDanmas(ctx context.Context, items []model.Danma) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, len(items))
	for i := range items {
		ids[i] = items[i].MessageID
	}
	var hidden []model.Danma
	if err := dao.SC.Db.WithContext(ctx).Select("message_id,hidden_at").Where("message_id IN ? AND hidden_at IS NOT NULL", ids).Find(&hidden).Error; err != nil {
		return err
	}
	set := map[string]*time.Time{}
	for _, d := range hidden {
		set[d.MessageID] = d.HiddenAt
	}
	for i := range items {
		if when, ok := set[items[i].MessageID]; ok {
			items[i].Content = "该弹幕已被隐藏"
			items[i].HiddenAt = when
		}
	}
	return nil
}
