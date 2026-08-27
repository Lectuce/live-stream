package model

type Danma struct {
	ID        uint64 `json:"-" gorm:"primaryKey;autoIncrement"`
	MessageID string `json:"message_id" gorm:"size:36;uniqueIndex;not null"`
	RoomID    int64  `json:"room_id"`
	UserID    int64  `json:"user_id"`
	Content   string `json:"content" gorm:"type:text"`
}
