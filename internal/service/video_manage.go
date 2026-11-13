package service

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"live-stream/internal/model"
	"live-stream/internal/repo"
	"os"
	"strings"
	"unicode/utf8"
)

var ErrVideoPublished = errors.New("unpublish the video before editing")
var ErrVideoParams = errors.New("invalid video metadata")

func editVideoMetadata(video *model.Video, req model.UpdateVideoRequest, autoApprove bool) error {
	if video.PublishStatus != "draft" {
		return ErrVideoPublished
	}
	if req.Title == nil && req.Description == nil {
		return ErrVideoParams
	}
	title, description := video.Title, video.Description
	if req.Title != nil {
		title = strings.TrimSpace(*req.Title)
	}
	if req.Description != nil {
		description = strings.TrimSpace(*req.Description)
	}
	if title == "" || utf8.RuneCountInString(title) > 100 || utf8.RuneCountInString(description) > 5000 {
		return ErrVideoParams
	}
	if title == video.Title && description == video.Description {
		return nil
	}
	video.Title, video.Description = title, description
	video.AuditStatus = "pending"
	if autoApprove && video.ProcessStatus == "ready" && video.PlayURL != "" {
		video.AuditStatus = "approved"
	}
	return nil
}

func EditVideo(ctx context.Context, id, ownerID uint64, req model.UpdateVideoRequest) (*model.OwnerVideo, error) {
	video, err := repo.MutateOwnerVideo(ctx, id, ownerID, func(v *model.Video) error {
		return editVideoMetadata(v, req, os.Getenv("VIDEO_UPLOAD_AUTO_APPROVE") == "1")
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrVideoNotFound
	}
	if err != nil {
		return nil, err
	}
	return &model.OwnerVideo{Video: *video, HasMedia: video.MediaFile != ""}, nil
}

func UnpublishVideo(ctx context.Context, id, ownerID uint64) (*model.OwnerVideo, error) {
	video, err := repo.MutateOwnerVideo(ctx, id, ownerID, func(v *model.Video) error {
		if v.PublishStatus != "published" && v.PublishStatus != "draft" {
			return ErrVideoNotReady
		}
		v.PublishStatus = "draft"
		v.PublishedAt = nil
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrVideoNotFound
	}
	if err != nil {
		return nil, err
	}
	return &model.OwnerVideo{Video: *video, HasMedia: video.MediaFile != ""}, nil
}

func DeleteVideo(ctx context.Context, id, ownerID uint64) error {
	rows, err := repo.SoftDeleteOwnerVideo(ctx, id, ownerID)
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrVideoNotFound
	}
	return nil
}
