package service

import (
	"context"
	"errors"
	"strconv"

	"live-stream/internal/model"
	"live-stream/internal/repo"
)

var ErrSelfFollow = errors.New("cannot follow yourself")

func GetPublicUser(ctx context.Context, id uint64) (*model.PublicUser, error) {
	user, err := repo.SelectPublicUser(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := repo.PublicUserCounts(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

func ListPublicAuthorVideos(ctx context.Context, ownerID, beforeID uint64, limit int) (*model.VideoPage, error) {
	if _, err := repo.SelectPublicUser(ctx, ownerID); err != nil {
		return nil, err
	}
	videos, err := repo.SelectPublicAuthorVideos(ctx, ownerID, beforeID, limit+1)
	if err != nil {
		return nil, err
	}
	page := &model.VideoPage{Items: videos, HasMore: len(videos) > limit}
	if page.HasMore {
		page.Items = videos[:limit]
		page.NextCursor = strconv.FormatUint(page.Items[limit-1].VideoID, 10)
	}
	return page, nil
}

func GetFollowState(ctx context.Context, actorID, targetID uint64) (*model.FollowState, error) {
	if _, err := repo.SelectPublicUser(ctx, targetID); err != nil {
		return nil, err
	}
	return repo.SelectFollowState(ctx, actorID, targetID)
}

func SetFollow(ctx context.Context, actorID, targetID uint64, follow bool) (*model.FollowState, error) {
	if actorID == targetID {
		return nil, ErrSelfFollow
	}
	if _, err := repo.SelectPublicUser(ctx, targetID); err != nil {
		return nil, err
	}
	if err := repo.SetUserFollow(ctx, actorID, targetID, follow); err != nil {
		return nil, err
	}
	return repo.SelectFollowState(ctx, actorID, targetID)
}
