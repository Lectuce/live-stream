package service

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"live-stream/internal/model"
	"live-stream/internal/repo"
	"strconv"
	"strings"
	"unicode/utf8"
)

var ErrReviewConflict = errors.New("video version or review state changed")
var ErrReviewInput = errors.New("invalid review request")

func ValidateReviewRequest(req *model.ReviewVideoRequest) error {
	req.Reason = strings.TrimSpace(req.Reason)
	if req.ContentVersion == 0 || (req.Decision != "approved" && req.Decision != "rejected") || utf8.RuneCountInString(req.Reason) > 1000 || (req.Decision == "rejected" && req.Reason == "") {
		return ErrReviewInput
	}
	if req.Decision == "approved" {
		req.Reason = ""
	}
	return nil
}

func checkReviewVersion(v *model.Video, version uint64) error {
	if v.DeletedAt.Valid || v.ContentVersion != version || v.AuditStatus != "pending" || v.ProcessStatus != "ready" || v.PublishStatus != "draft" || v.MediaFile == "" {
		return ErrReviewConflict
	}
	return nil
}

func ReviewVideo(ctx context.Context, id, reviewerID uint64, req model.ReviewVideoRequest) error {
	if err := ValidateReviewRequest(&req); err != nil {
		return err
	}
	err := repo.ReviewVideoTransaction(ctx, id, reviewerID, req, func(v *model.Video) error { return checkReviewVersion(v, req.ContentVersion) })
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrVideoNotFound
	}
	return err
}

func ListReviewVideos(ctx context.Context, status string, beforeID uint64, limit int) (*model.ReviewVideoPage, error) {
	if status != "pending" && status != "approved" && status != "rejected" {
		return nil, ErrReviewInput
	}
	rows, err := repo.SelectReviewVideos(ctx, status, beforeID, limit+1)
	if err != nil {
		return nil, err
	}
	page := &model.ReviewVideoPage{Items: make([]model.ReviewVideo, 0), HasMore: len(rows) > limit}
	if page.HasMore {
		rows = rows[:limit]
		page.NextCursor = strconv.FormatUint(rows[len(rows)-1].VideoID, 10)
	}
	names, err := repo.SelectVideoAuthorNames(ctx, rows)
	if err != nil {
		return nil, err
	}
	for _, v := range rows {
		page.Items = append(page.Items, model.ReviewVideo{OwnerVideo: model.ToOwnerVideo(v), OwnerUsername: names[v.OwnerID]})
	}
	return page, nil
}

func GetReviewVideo(ctx context.Context, id uint64) (*model.ReviewVideoDetail, error) {
	v, err := repo.SelectVideo(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrVideoNotFound
	}
	if err != nil {
		return nil, err
	}
	names, err := repo.SelectVideoAuthorNames(ctx, []model.Video{*v})
	if err != nil {
		return nil, err
	}
	reviews, err := repo.SelectVideoReviews(ctx, id)
	if err != nil {
		return nil, err
	}
	return &model.ReviewVideoDetail{ReviewVideo: model.ReviewVideo{OwnerVideo: model.ToOwnerVideo(*v), OwnerUsername: names[v.OwnerID]}, Reviews: reviews}, nil
}

func GetReviewMedia(ctx context.Context, id, version uint64) (*model.Video, error) {
	v, err := repo.SelectVideo(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrVideoNotFound
	}
	if err != nil {
		return nil, err
	}
	if v.ContentVersion != version {
		return nil, ErrReviewConflict
	}
	if v.ProcessStatus != "ready" || v.MediaFile == "" {
		return nil, ErrVideoNotFound
	}
	return v, nil
}
