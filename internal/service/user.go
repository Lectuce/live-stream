package service

import (
	"context"
	"errors"
	"live-stream/internal/model"
	"live-stream/internal/repo"
	"log"
)

func GetUserByID(c context.Context, userID uint64) (*model.User, error) {
	var user model.User
	user = model.User{
		UserID: userID,
	}
	reuser, err := repo.SelectUser(c, &user)
	if err != nil {
		log.Printf("get user by id failed: %v", err)
		return nil, err
	}
	if reuser == nil {
		return nil, errors.New("user is not exist")
	}
	return reuser, nil
}
