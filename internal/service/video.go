package service

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"

	"live-stream/internal/model"
	"live-stream/internal/repo"

	"gorm.io/gorm"
)

var (
	ErrVideoNotFound  = errors.New("video not found")
	ErrVideoForbidden = errors.New("video forbidden")
	ErrVideoNotReady  = errors.New("video not ready")
	ErrVideoTitle     = errors.New("video title is empty")
)

func CreateVideo(
	ctx context.Context,
	ownerID uint64,
	req model.CreateVideoRequest,
) (*model.Video, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return nil, ErrVideoTitle
	}

	video := &model.Video{
		OwnerID:       ownerID,
		Title:         title,
		Description:   strings.TrimSpace(req.Description),
		ProcessStatus: "pending",
		PublishStatus: "draft",
		AuditStatus:   "pending",
	}

	// 仅用于当前固定 MP4 的开发验证。
	// 不接收客户端提交的 ready/approved 或任意播放地址。
	if os.Getenv("VIDEO_DEMO_MODE") == "1" {
		video.PlayURL = "/media/output.mp4"
		video.ProcessStatus = "ready"
		video.AuditStatus = "approved"
	}

	if err := repo.CreateVideo(ctx, video); err != nil {
		return nil, err
	}
	return video, nil
}

func GetPublicVideo(
	ctx context.Context,
	videoID uint64,
) (*model.Video, error) {
	video, err := repo.SelectVideo(ctx, videoID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrVideoNotFound
	}
	if err != nil {
		return nil, err
	}

	if video.PublishStatus != "published" ||
		video.ProcessStatus != "ready" ||
		video.AuditStatus != "approved" {
		return nil, ErrVideoNotFound
	}

	return video, nil
}

func GetOwnerVideo(ctx context.Context, videoID, ownerID uint64) (*model.OwnerVideo, error) {
	video, err := repo.SelectOwnerVideo(ctx, videoID, ownerID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrVideoNotFound
	}
	if err != nil {
		return nil, err
	}
	result := model.ToOwnerVideo(*video)
	return &result, nil
}

func ListOwnerVideos(ctx context.Context, ownerID, beforeID uint64, limit int) (*model.OwnerVideoPage, error) {
	videos, err := repo.SelectOwnerVideos(ctx, ownerID, beforeID, limit+1)
	if err != nil {
		return nil, err
	}
	page := &model.OwnerVideoPage{Items: make([]model.OwnerVideo, 0), HasMore: len(videos) > limit}
	if page.HasMore {
		videos = videos[:limit]
		page.NextCursor = strconv.FormatUint(videos[len(videos)-1].VideoID, 10)
	}
	for _, video := range videos {
		page.Items = append(page.Items, model.ToOwnerVideo(video))
	}
	return page, nil
}

func ListVideos(
	ctx context.Context,
	beforeID uint64,
	limit int,
) (*model.VideoPage, error) {
	items, err := repo.SelectPublishedVideos(ctx, beforeID, limit+1)
	if err != nil {
		return nil, err
	}

	hasMore := len(items) > limit
	nextCursor := ""

	if hasMore {
		items = items[:limit]
		nextCursor = strconv.FormatUint(items[len(items)-1].VideoID, 10)
	}

	return &model.VideoPage{
		Items:      items,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

func PublishVideo(
	ctx context.Context,
	videoID uint64,
	ownerID uint64,
) error {
	video, err := repo.SelectVideo(ctx, videoID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrVideoNotFound
	}
	if err != nil {
		return err
	}
	if video.OwnerID != ownerID {
		return ErrVideoForbidden
	}
	if video.ProcessStatus != "ready" ||
		video.AuditStatus != "approved" ||
		video.PlayURL == "" {
		return ErrVideoNotReady
	}
	if video.PublishStatus == "published" {
		return nil
	}
	if video.PublishStatus != "draft" {
		return ErrVideoNotReady
	}

	rows, err := repo.PublishVideo(ctx, videoID, ownerID)
	if err != nil {
		return err
	}
	if rows > 0 {
		return nil
	}

	// 并发重复发布时，已经发布成功也视为成功。
	current, err := repo.SelectVideo(ctx, videoID)
	if err != nil {
		return err
	}
	if current.OwnerID == ownerID &&
		current.PublishStatus == "published" &&
		current.ProcessStatus == "ready" &&
		current.AuditStatus == "approved" {
		return nil
	}

	return ErrVideoNotReady
}
