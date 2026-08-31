package service

import (
	"context"
	"errors"
	"fmt"
	"live-stream/internal/model"
	"live-stream/internal/repo"
	"log"
)

var ErrInvalidEamil = errors.New("the email has been registered")

func UserReigister(ctx context.Context, registerRequest *model.RegisterRequest) (*model.User, error) {
	if registerRequest == nil {
		return nil, errors.New("register request is nil")
	}
	hash, err := HashPassword(registerRequest.Password)
	if err != nil {
		log.Printf("create hash password failed: %v", err)
		return nil, err
	}
	var user model.User
	user = model.User{
		UserName:     registerRequest.UserName,
		UserEmail:    registerRequest.UserEmail,
		PasswordHash: hash,
	}
	reuser, err := repo.SelectUserByEmail(ctx, &user)
	if err != nil {
		return nil, fmt.Errorf("select user by email: %w", err)
	}
	if reuser != nil {
		return nil, ErrInvalidEamil
	}

	reuser, err = repo.CreateUser(ctx, &user)
	if err != nil {
		log.Printf("create user failed: %v", err)
		return nil, fmt.Errorf("create user: %w", err)
	}
	return reuser, nil

}
