package model

type RegisterRequest struct {
	UserName  string `json:"username" binding:"required,min=2,max=32"`
	UserEmail string `json:"email" binding:"required,email,max=254"`
	Password  string `json:"password" binding:"required,min=8,max=128"`
}
