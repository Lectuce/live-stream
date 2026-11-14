package model

import (
	"gorm.io/gorm"
	"time"
)

type Post struct {
	PostID      uint64         `json:"post_id,string" gorm:"primaryKey;autoIncrement"`
	OwnerID     uint64         `json:"owner_id,string" gorm:"not null;index"`
	Content     string         `json:"content" gorm:"type:text;not null"`
	Status      string         `json:"status" gorm:"size:20;not null;default:draft"`
	Version     uint64         `json:"version,string" gorm:"not null;default:1"`
	PublishedAt *time.Time     `json:"published_at" gorm:"type:datetime(6)"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}

type PostImage struct {
	ID        string    `json:"image_id" gorm:"primaryKey;size:36"`
	OwnerID   uint64    `json:"-" gorm:"not null;index"`
	PostID    uint64    `json:"-" gorm:"not null;default:0;index"`
	Position  int       `json:"-"`
	Filename  string    `json:"-" gorm:"size:50;not null"`
	CreatedAt time.Time `json:"-"`
}

type PostImageView struct {
	ID  string `json:"image_id"`
	URL string `json:"url"`
}

type PostView struct {
	Post
	Images []PostImageView `json:"images"`
}

type EditPostRequest struct {
	Version  uint64   `json:"version,string"`
	Content  string   `json:"content"`
	ImageIDs []string `json:"image_ids"`
}
