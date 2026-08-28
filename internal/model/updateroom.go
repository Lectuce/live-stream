package model

type UpdateRoomRequest struct {
	RoomID    uint64 `json:"room_id" gorm:"primaryKey;autoIncrement"`
	RoomTitle string `json:"room_title" mapstructure:"room_title"`
}
