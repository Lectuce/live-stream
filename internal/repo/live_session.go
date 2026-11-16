package repo

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"live-stream/internal/dao"
	"live-stream/internal/model"
	"time"
)

var ErrLiveConflict = errors.New("live session is no longer available")

func PrepareLive(ctx context.Context, roomID, owner uint64, title string) (*model.LiveSession, error) {
	var session model.LiveSession
	err := dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var room model.Room
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("room_id = ? AND owner_id = ?", roomID, owner).First(&room).Error; err != nil {
			return err
		}
		if room.LiveSessionID != 0 {
			if err := tx.First(&session, room.LiveSessionID).Error; err != nil {
				return err
			}
			if session.Status == "live" || session.Status == "pending" && time.Now().Before(session.ExpiresAt) {
				return nil
			}
			if session.Status == "pending" {
				if err := tx.Model(&session).Updates(map[string]any{"status": "ended", "ended_at": time.Now()}).Error; err != nil {
					return err
				}
			}
		}
		session = model.LiveSession{RoomID: roomID, OwnerID: owner, Stream: uuid.NewString(), Title: title, Status: "pending", ExpiresAt: time.Now().Add(15 * time.Minute)}
		if err := tx.Create(&session).Error; err != nil {
			return err
		}
		return tx.Model(&room).Updates(map[string]any{"live_session_id": session.ID, "status": 0, "play_url": "", "room_title": title}).Error
	})
	return &session, err
}

// All mutations lock the room first, then the session, so callbacks and owner actions serialize.
func ApplyLiveCallback(ctx context.Context, stream, client string, publish bool, playURL string) (*model.LiveSession, error) {
	var session model.LiveSession
	if err := dao.SC.Db.WithContext(ctx).Where("stream = ?", stream).First(&session).Error; err != nil {
		return nil, err
	}
	err := dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var room model.Room
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("room_id = ?", session.RoomID).First(&room).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&session, session.ID).Error; err != nil {
			return err
		}
		if publish {
			if room.LiveSessionID != session.ID || !CanPublishLive(session, client, time.Now()) {
				return ErrLiveConflict
			}
			if session.Status == "live" {
				return nil
			}
			now := time.Now()
			session.Status = "live"
			session.ClientID = client
			session.StartedAt = &now
			if err := tx.Save(&session).Error; err != nil {
				return err
			}
			if err := tx.Model(&room).Updates(map[string]any{"status": 1, "start_at": now, "play_url": playURL}).Error; err != nil {
				return err
			}
			return QueueContentEvent(tx, "live.started", session.OwnerID, session.ID, now)
		}
		// A stop can arrive before a delayed publish callback. Tombstone pending
		// sessions too so the delayed publish cannot bring a disconnected stream live.
		if session.Status == "ended" {
			return nil
		}
		if session.Status == "live" && session.ClientID != client {
			return nil
		}
		return endLive(tx, &room, &session)
	})
	return &session, err
}

func CanPublishLive(s model.LiveSession, client string, now time.Time) bool {
	return client != "" && ((s.Status == "pending" && now.Before(s.ExpiresAt)) || (s.Status == "live" && s.ClientID == client))
}

func endLive(tx *gorm.DB, room *model.Room, session *model.LiveSession) error {
	now := time.Now()
	session.Status = "ended"
	session.EndedAt = &now
	if err := tx.Save(session).Error; err != nil {
		return err
	}
	if room.LiveSessionID != session.ID {
		return nil
	}
	return tx.Model(room).Updates(map[string]any{"status": 0, "play_url": ""}).Error
}

func EndLive(ctx context.Context, roomID, owner uint64) (*model.LiveSession, error) {
	var s model.LiveSession
	err := dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var room model.Room
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("room_id = ? AND owner_id = ?", roomID, owner).First(&room).Error; err != nil {
			return err
		}
		if room.LiveSessionID == 0 {
			return tx.Model(&room).Updates(map[string]any{"status": 0, "play_url": ""}).Error
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&s, room.LiveSessionID).Error; err != nil {
			return err
		}
		if s.Status == "ended" {
			return nil
		}
		return endLive(tx, &room, &s)
	})
	return &s, err
}
