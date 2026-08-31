package repo

import (
	"context"
	"fmt"
	"live-stream/internal/dao"
	"live-stream/internal/model"

	"gorm.io/gorm"
)

func CreateUser(ctx context.Context, user *model.User) (*model.User, error) {
	// err := gorm.G[model.User](dao.SC.Db).Create(ctx, user)
	// if err != nil {
	// 	panic(fmt.Errorf("fatal error create user: %w", err))
	// }
	results := gorm.WithResult()
	err := gorm.G[model.User](dao.SC.Db, results).Create(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("fatal error create user: %w", err)
	}
	if results.RowsAffected == 0 {
		return nil, fmt.Errorf("no rows inserted")
	}
	return user, nil
}

func DeleteUser(ctx context.Context, user *model.User) error {
	rowsaffected, err := gorm.G[model.User](dao.SC.Db).Where("user_id = ? AND user_name = ?", user.UserID, user.UserName).Delete(ctx)
	if err != nil {
		return fmt.Errorf("fatal error delete user: %w", err)
	}
	if rowsaffected != 0 {
		return nil
	} else {
		return fmt.Errorf("no record found, id: %v, name: %v", user.UserID, user.UserName)
	}

}

func UpdateUser(ctx context.Context, user *model.User) error {
	rowsaffected, err := gorm.G[model.User](dao.SC.Db).Where("user_id = ?", user.UserID).Update(ctx, "user_name", user.UserName)
	if err != nil {
		return fmt.Errorf("fatal error update user: %w", err)
	}
	if rowsaffected != 0 {
		return nil
	} else {
		return fmt.Errorf("no record found, id: %v, name: %v", user.UserID, user.UserName)
	}
}

func SelectUsers(ctx context.Context, user *model.User) ([]model.User, error) {

	users, err := gorm.G[model.User](dao.SC.Db).Where("user_name LIKE ?", "%"+user.UserName+"%").Find(ctx)

	if err != nil {
		return nil, fmt.Errorf("fatal error select user: %w", err)
	}

	return users, nil
}

func SelectUser(ctx context.Context, user *model.User) (*model.User, error) {

	resuser, err := gorm.G[model.User](dao.SC.Db).Where("user_id = ?", user.UserID).First(ctx)

	if err != nil {
		return nil, fmt.Errorf("fatal error select user: %w", err)
	}

	return &resuser, nil
}

func SelectUserByEmail(ctx context.Context, user *model.User) (*model.User, error) {

	resuser, err := gorm.G[model.User](dao.SC.Db).Where("user_email = ?", user.UserEmail).First(ctx)

	if err != nil {
		return nil, fmt.Errorf("fatal error select user: %w", err)
	}

	return &resuser, nil
}

func SelectUserByUserID(ctx context.Context, userID uint64) (*model.User, error) {

	resuser, err := gorm.G[model.User](dao.SC.Db).Where("user_id = ?", userID).First(ctx)

	if err != nil {
		return nil, fmt.Errorf("fatal error select user: %w", err)
	}

	return &resuser, nil
}
