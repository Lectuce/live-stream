package model

type RegisterRequest struct {
	UserName  string `json:"user_name"`
	UserEmail string `json:"user_emal"`
	Password  string `json:"pass_word"`
}
