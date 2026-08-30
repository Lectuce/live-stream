package model

type LoginRequest struct {
	UserEmail string `json:"email" binding:"required,email"`
	Password  string `json:"password" binding:"required,min=8,max=128"`
}
