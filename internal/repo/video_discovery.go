package repo

import (
	"context"
	"database/sql"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"live-stream/internal/dao"
	"live-stream/internal/model"
)

func publicVideoCards(db *gorm.DB) *gorm.DB {
	return db.Table("videos v").Select("v.video_id,v.owner_id,u.user_name AS owner_name,v.title,v.cover_url,v.category,v.duration_ms,v.published_at").Joins("JOIN users u ON u.user_id=v.owner_id").Where("v.deleted_at IS NULL AND v.publish_status='published' AND v.process_status='ready' AND v.audit_status='approved' AND v.published_at IS NOT NULL")
}
func VideoCards(ctx context.Context, ids []uint64, category string) ([]model.VideoCard, error) {
	items := make([]model.VideoCard, 0)
	if len(ids) == 0 {
		return items, nil
	}
	q := publicVideoCards(dao.SC.Db.WithContext(ctx)).Where("v.video_id IN ?", ids)
	if category != "" {
		q = q.Where("v.category = ?", category)
	}
	err := q.Scan(&items).Error
	return items, err
}
func LatestVideoCards(ctx context.Context, category string, before time.Time, id uint64, limit int) ([]model.VideoCard, error) {
	items := make([]model.VideoCard, 0)
	q := publicVideoCards(dao.SC.Db.WithContext(ctx))
	if category != "" {
		q = q.Where("v.category = ?", category)
	}
	if id != 0 {
		q = q.Where("(v.published_at < ? OR (v.published_at = ? AND v.video_id < ?))", before, before, id)
	}
	err := q.Order("v.published_at DESC,v.video_id DESC").Limit(limit).Scan(&items).Error
	return items, err
}
func HotVideoCandidates(ctx context.Context, category string) ([]uint64, error) {
	ids := make([]uint64, 0)
	q := dao.SC.Db.WithContext(ctx).Table("video_metrics m").Select("m.video_id").Joins("JOIN videos v ON v.video_id=m.video_id").Where("m.day >= ? AND v.deleted_at IS NULL AND v.publish_status='published' AND v.process_status='ready' AND v.audit_status='approved'", time.Now().UTC().AddDate(0, 0, -6).Format("2006-01-02"))
	if category != "" {
		q = q.Where("v.category = ?", category)
	}
	err := q.Group("m.video_id").Order("SUM(m.plays) DESC,m.video_id DESC").Limit(250).Scan(&ids).Error
	return ids, err
}
func VideoRankingSignals(ctx context.Context, ids []uint64) (map[uint64]model.VideoSignals, error) {
	result := make(map[uint64]model.VideoSignals)
	if len(ids) == 0 {
		return result, nil
	}
	db := dao.SC.Db.WithContext(ctx)
	since := time.Now().UTC().AddDate(0, 0, -6)
	var metrics []model.VideoSignals
	err := db.Table("video_metrics").Select("video_id,SUM(plays * POW(0.8,DATEDIFF(?,day))) AS plays,SUM(clicks * POW(0.8,DATEDIFF(?,day))) AS clicks", time.Now().UTC().Format("2006-01-02"), time.Now().UTC().Format("2006-01-02")).Where("day >= ? AND video_id IN ?", since.Format("2006-01-02"), ids).Group("video_id").Scan(&metrics).Error
	if err != nil {
		return nil, err
	}
	for _, m := range metrics {
		result[m.VideoID] = m
	}
	// Interactions come from committed business tables, never browser counters.
	queries := []*gorm.DB{
		db.Table("content_likes").Select("target_id AS video_id,COUNT(*) AS likes").Where("target_type='video' AND target_id IN ? AND created_at >= ?", ids, since).Group("target_id"),
		db.Table("favorite_items f").Select("f.video_id,COUNT(DISTINCT d.owner_id) AS favorites").Joins("JOIN favorite_folders d ON d.id=f.folder_id").Where("f.video_id IN ? AND f.created_at >= ?", ids, since).Group("f.video_id"),
		db.Table("comments").Select("target_id AS video_id,COUNT(DISTINCT user_id) AS comments").Where("target_type='video' AND target_id IN ? AND deleted_at IS NULL AND audit_status='approved' AND created_at >= ?", ids, since).Group("target_id"),
	}
	for _, q := range queries {
		var rows []model.VideoSignals
		if err := q.Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, v := range rows {
			r := result[v.VideoID]
			r.Likes += v.Likes
			r.Favorites += v.Favorites
			r.Comments += v.Comments
			result[v.VideoID] = r
		}
	}
	return result, nil
}
func recordVideoBehavior(tx *gorm.DB, actor, video uint64, kind string) error {
	day := time.Now().UTC().Format("2006-01-02")
	metric := model.VideoMetric{Day: day, VideoID: video}
	column := ""
	switch kind {
	case "impression":
		metric.Impressions = 1
		column = "impressions"
	case "click":
		metric.Clicks = 1
		column = "clicks"
	case "play":
		metric.Plays = 1
		column = "plays"
	default:
		return ErrWatchInput
	}
	// The configured MySQL DSN uses affected-row semantics (ClientFoundRows=false).
	// A duplicate no-op returns zero; normal retries should not emit SQL errors.
	insert := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.VideoBehavior{Day: day, UserID: actor, VideoID: video, Kind: kind})
	if insert.Error != nil {
		return insert.Error
	}
	if insert.RowsAffected == 0 {
		return nil
	}
	return tx.Clauses(clause.OnConflict{DoUpdates: clause.Assignments(map[string]any{column: gorm.Expr(column + " + 1")})}).Create(&metric).Error
}
func RecordVideoBehavior(ctx context.Context, actor, video uint64, kind string) error {
	return dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := watchVideo(tx, video); err != nil {
			return err
		}
		return recordVideoBehavior(tx, actor, video, kind)
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}
func StartVideoPlay(ctx context.Context, actor, video uint64) (*model.VideoPlaySession, error) {
	now := time.Now().UTC()
	session := &model.VideoPlaySession{ID: uuid.NewString(), UserID: actor, VideoID: video, UpdatedAt: now, ExpiresAt: now.Add(30 * time.Minute)}
	err := dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := watchVideo(tx, video); err != nil {
			return err
		}
		return tx.Create(session).Error
	})
	return session, err
}

// Credit only plausible forward movement, bounded by server time and one 15s
// sampling interval. Seeking, pausing and duplicate sequences do not earn time.
func playCredit(previous, current uint64, elapsed time.Duration) uint64 {
	if current <= previous || elapsed <= 0 || elapsed > 20*time.Second {
		return 0
	}
	delta := current - previous
	wall := uint64(elapsed.Milliseconds())
	if delta > wall*2+1000 {
		return 0
	}
	return min(delta, wall, 15000)
}
func PulseVideoPlay(ctx context.Context, actor, video uint64, id string, seq, position uint64) (bool, error) {
	qualified := false
	err := dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		v, err := watchVideo(tx, video)
		if err != nil {
			return err
		}
		var s model.VideoPlaySession
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ? AND video_id = ?", id, actor, video).First(&s).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		if now.After(s.ExpiresAt) {
			return ErrWatchConflict
		}
		if position > v.DurationMS || v.DurationMS == 0 || seq == 0 || seq > 9007199254740991 {
			return ErrWatchInput
		}
		if seq == s.LastSeq && position != s.PositionMS {
			return ErrWatchInput
		}
		if seq > s.LastSeq {
			s.WatchedMS += playCredit(s.PositionMS, position, now.Sub(s.UpdatedAt))
			s.PositionMS = position
			s.LastSeq = seq
			s.UpdatedAt = now
			if err := tx.Save(&s).Error; err != nil {
				return err
			}
		}
		qualified = s.WatchedMS >= uint64(math.Ceil(math.Min(30000, float64(v.DurationMS)*0.8)))
		if qualified {
			return recordVideoBehavior(tx, actor, video, "play")
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	return qualified, err
}

func SearchDiscovery(ctx context.Context, kind, term string, before uint64, limit int) ([]model.SearchItem, error) {
	items := make([]model.SearchItem, 0)
	// Search a bounded, recent candidate set; no unbounded %LIKE% table scans.
	term = "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(term) + "%"
	var query string
	switch kind {
	case "video":
		query = "SELECT video_id AS id,title,cover_url AS image FROM (SELECT video_id,title,cover_url FROM videos WHERE deleted_at IS NULL AND publish_status='published' AND process_status='ready' AND audit_status='approved' ORDER BY video_id DESC LIMIT 5000) candidate WHERE title LIKE ? ESCAPE '!'"
	case "user":
		query = "SELECT user_id AS id,user_name AS title,avatar_url AS image FROM (SELECT user_id,user_name,avatar_url FROM users ORDER BY user_id DESC LIMIT 5000) candidate WHERE user_name LIKE ? ESCAPE '!'"
	case "live":
		query = "SELECT room_id AS id,room_title AS title,'' AS image FROM (SELECT room_id,room_title FROM rooms WHERE status=1 ORDER BY room_id DESC LIMIT 5000) candidate WHERE room_title LIKE ? ESCAPE '!'"
	default:
		return nil, ErrWatchInput
	}
	query = "SELECT * FROM (" + query + ") matches WHERE (? = 0 OR id < ?) ORDER BY id DESC LIMIT ?"
	err := dao.SC.Db.WithContext(ctx).Raw(query, term, before, before, limit).Scan(&items).Error
	for i := range items {
		items[i].Kind = kind
	}
	return items, err
}

// Bounded maintenance, invoked by the shared feed rebuild lease. Old records
// never participate in ranking; this avoids an unbounded startup deletion.
func PruneVideoDiscovery(ctx context.Context) {
	for _, q := range []string{"DELETE FROM video_play_sessions WHERE expires_at < ? LIMIT 1000", "DELETE FROM video_behaviors WHERE day < ? LIMIT 1000", "DELETE FROM video_metrics WHERE day < ? LIMIT 1000"} {
		cutoff := any(time.Now().UTC())
		if strings.Contains(q, "day <") {
			cutoff = time.Now().UTC().AddDate(0, 0, -14).Format("2006-01-02")
		}
		if dao.SC.Db.WithContext(ctx).Exec(q, cutoff).Error != nil {
			return
		}
	}
}
