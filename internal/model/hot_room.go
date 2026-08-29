package model

type HotRoomDTO struct {
	RoomID        uint64 `json:"room_id"`
	RoomTitle     string `json:"room_title"`
	OwnerID       int64  `json:"owner_id"`
	OwnerUsername string `json:"owner_username"`
	PlayURL       string `json:"play_url"`
	PraiseCount   int64  `json:"praise_count"`
}
