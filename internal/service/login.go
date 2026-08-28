package service

import (
	"context"
	"errors"
	"fmt"
	"live-stream/internal/auth"
	"live-stream/internal/model"
	"live-stream/internal/repo"
	"log"
)

var ErrInvalidCredentials = errors.New("the email or password is wrong")

func UserLogin(ctx context.Context, loginRequest *model.LoginRequest) (*model.User, string, error) {
	if loginRequest == nil {
		return nil, "", errors.New("the email is not registered")
	}

	var user model.User
	user = model.User{
		UserEmail: loginRequest.UserEmail,
	}

	reuser, err := repo.SelectUserByEmail(ctx, &user)
	if err != nil {
		log.Printf("login failed: %v", err)
		return nil, "", fmt.Errorf("select user by email: %w", err)
	}
	if reuser == nil {
		return nil, "", ErrInvalidCredentials
	}
	match, err := VerifyPassword(loginRequest.Password, reuser.PasswordHash)
	if err != nil {
		log.Printf("verify password failed: %v", err)
		return nil, "", fmt.Errorf("verify password: %w", err)
	}
	if !match {
		return nil, "", ErrInvalidCredentials
	}
	// 生成会话token
	token, err := auth.GenSessionToken()
	if err != nil {
		log.Printf("generate session token failed: %v", err)
		return nil, "", fmt.Errorf("gen token: %w", err)
	}

	// value存用户ID
	err = auth.StoreSession(ctx, token, reuser.UserID)
	if err != nil {
		log.Printf("redis save session failed: %v", err)
		return nil, "", fmt.Errorf("save session: %w", err)
	}

	return reuser, token, nil
}
