package model

type Message struct {
	MessageID string `json:"message_id"`
	RoomID    uint64 `json:"room_id"` // 该消息发给哪一个房间
	UserID    uint64 `json:"user_id"` // 该消息的发送者用户 ID
	UserName  string `json:"user_name"`
	Content   string `json:"content"`
}
