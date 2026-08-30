package model

type UpdateRoomRequest struct {
	RoomTitle string `json:"room_title" mapstructure:"room_title" binding:"required,min=1,max=100"`
}
