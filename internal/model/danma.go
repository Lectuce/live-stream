package model

type Danma struct {
	RoomID  int64  `json:"room_id"`
	UserID  int64  `json:"user_id"`
	Content string `json:"content_id"`
}
