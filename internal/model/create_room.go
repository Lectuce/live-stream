package model

type CreateRoomRequest struct {
	Title string `json:"title" binding:"required,min=1,max=100"`
}
