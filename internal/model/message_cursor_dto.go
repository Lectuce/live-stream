package model

type MessageCursorDTO struct {
	List       []Danma `json:"list"`
	NextCursor uint64  `json:"next_cursor"`
	HasMore    bool    `json:"has_more"`
}
