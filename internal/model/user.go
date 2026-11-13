package model

import "time"

type User struct {
	Role         string    `json:"role" gorm:"size:20;not null;default:user"`
	UserID       uint64    `json:"user_id" gorm:"primaryKey;autoIncrement"`
	UserName     string    `json:"user_name" mapstructure:"user_name" gorm:"size:32;uniqueIndex:idx_users_username;not null"`
	UserEmail    string    `json:"user_email" mapstructure:"email" gorm:"size:254;uniqueIndex:idx_users_email;not null"`
	PasswordHash string    `json:"-" mapstructure:"-" gorm:"size:255;not null"`
	AvatarURL    string    `json:"avatar_url" mapstructure:"avatar_url" gorm:"size:512"`
	CreatedAt    time.Time `json:"created_at" mapstructure:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" mapstructure:"updated_at"`
}
