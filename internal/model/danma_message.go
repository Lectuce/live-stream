package model

import "time"

const (
	MsgTypeDanma    = "danma"
	MsgTypeLike     = "like"
	MsgTypeSystem   = "system"
	MsgTypeAck      = "ack"
	MsgTypeAccepted = "accepted"
)

type Message struct {
	ID        uint64                 `json:"id,omitempty"`
	MessageID string                 `json:"message_id"`
	RoomID    uint64                 `json:"room_id"` // 该消息发给哪一个房间
	RoomSeq   uint64                 `json:"room_seq,omitempty"`
	AckSeq    uint64                 `json:"ack_seq,omitempty"`
	UserID    uint64                 `json:"user_id"` // 该消息的发送者用户 ID
	UserName  string                 `json:"user_name"`
	Content   string                 `json:"content"`
	Type      string                 `json:"type"`
	Data      map[string]interface{} `json:"data,omitempty"`
	CreatedAt *time.Time             `json:"created_at,omitempty"`
}
