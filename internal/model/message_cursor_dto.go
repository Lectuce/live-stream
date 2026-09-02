package model

type MessageCursorDTO struct {
	Items      []Danma `json:"items"`
	NextCursor uint64  `json:"next_cursor"`
	HasMore    bool    `json:"has_more"`
}

type MessageReplayDTO struct {
	Items   []Danma `json:"items"`
	NextSeq uint64  `json:"next_seq"`
	HasMore bool    `json:"has_more"`
	Source  string  `json:"source"`
}
