package model

type MessageCursorDTO struct {
	Items       []Danma `json:"items"`
	NextCursor uint64  `json:"next_cursor"`
	HasMore    bool    `json:"has_more"`
}
