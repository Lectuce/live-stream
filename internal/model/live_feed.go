package model

type LiveFeedPage struct {
	Items      []HotRoomDTO `json:"items"`
	NextCursor string       `json:"next_cursor"`
	HasMore    bool         `json:"has_more"`
}
