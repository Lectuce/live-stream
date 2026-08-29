package model

type CreateRoomRequest struct {
	OwnerID uint64 `json:"owner_id" mapstructure:"owner_id"`
	PushURL string `json:"-" mapstructure:"push_url"`
}
