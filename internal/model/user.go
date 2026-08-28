package model

import "time"

type User struct {
	UserID       uint64    `json:"user_id" gorm:"primaryKey;autoIncrement"`
	UserName     string    `json:"user_name" mapstructure:"user_name"`
	UserEmail    string    `json:"usereamil" mapstructure:"email"`
	PasswordHash string    `json:"-" mapstructure:"-"`
	AvatarURL    string    `json:"avatar_url" mapstructure:"avatar_url"`
	CreatedAt    time.Time `json:"created_at" mapstructure:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" mapstructure:"updated_at"`
}
