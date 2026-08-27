package ws

type Message struct {
	MessageID string `json:"message_id"`
	RoomID    int64  `json:"room_id"` // 该消息发给哪一个房间
	UserID    int64  `json:"user_id"` // 该消息的发送者用户 ID
	Content   string `json:"content"`
}

func NewMessage(roomID int64, userID int64, content string) *Message {
	return &Message{
		RoomID:  roomID,
		UserID:  userID,
		Content: content,
	}
}
