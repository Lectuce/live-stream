package model

import "time"

type HotRoomDTO struct {
	RoomID        uint64  `json:"room_id"`
	RoomTitle     string  `json:"room_title"`
	OwnerID       int64   `json:"owner_id"`
	OwnerUsername string  `json:"owner_username"`
	PlayURL       string  `json:"play_url"`
	LikeCount     int64   `json:"like_count"`
	HotScore      float64 `json:"hot_score"`
}

type ActiveRoomHeatSource struct {
	RoomID  uint64     `json:"room_id"`
	StartAt *time.Time `json:"start_at"`
}
