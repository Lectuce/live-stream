package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"live-stream/internal/model"
	"live-stream/internal/repo"
)

var ErrUnsupportedVideo = errors.New("unsupported video: use MP4 with H.264/yuv420p and optional AAC audio, up to 1080p and 1 hour")

func VideoStorageDir() (string, error) {
	dir := os.Getenv("VIDEO_STORAGE_DIR")
	if dir == "" {
		dir = "storage/videos"
	}
	return filepath.Abs(dir)
}

func VideoUploadDraft(ctx context.Context, id, ownerID uint64) (*model.Video, error) {
	v, err := repo.SelectVideo(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrVideoNotFound
	}
	if err != nil {
		return nil, err
	}
	if v.OwnerID != ownerID {
		return nil, ErrVideoForbidden
	}
	if v.PublishStatus != "draft" || v.MediaFile != "" {
		return nil, ErrVideoNotReady
	}
	return v, nil
}

type probeResult struct {
	Format struct {
		Name     string `json:"format_name"`
		Duration string `json:"duration"`
	} `json:"format"`
	Streams []struct {
		Type        string `json:"codec_type"`
		Codec       string `json:"codec_name"`
		PixelFormat string `json:"pix_fmt"`
		Width       int    `json:"width"`
		Height      int    `json:"height"`
	} `json:"streams"`
}

func validateProbe(payload []byte) (uint64, error) {
	var result probeResult
	if json.Unmarshal(payload, &result) != nil {
		return 0, ErrUnsupportedVideo
	}
	seconds, err := strconv.ParseFloat(result.Format.Duration, 64)
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 || seconds > 3600 {
		return 0, ErrUnsupportedVideo
	}
	if !strings.Contains(","+result.Format.Name+",", ",mp4,") {
		return 0, ErrUnsupportedVideo
	}
	videos, audios := 0, 0
	for _, stream := range result.Streams {
		switch stream.Type {
		case "video":
			videos++
			if stream.Codec != "h264" || stream.PixelFormat != "yuv420p" || stream.Width < 1 || stream.Height < 1 || stream.Width > 1920 || stream.Height > 1920 || int64(stream.Width)*int64(stream.Height) > 1920*1080 {
				return 0, ErrUnsupportedVideo
			}
		case "audio":
			audios++
			if stream.Codec != "aac" {
				return 0, ErrUnsupportedVideo
			}
		default:
			return 0, ErrUnsupportedVideo
		}
	}
	if videos != 1 || audios > 1 {
		return 0, ErrUnsupportedVideo
	}
	return uint64(math.Ceil(seconds * 1000)), nil
}

// Cap subprocess output even for malicious or malformed media.
type cappedProbeOutput struct{ bytes.Buffer }

func (b *cappedProbeOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 64*1024 {
		return 0, fmt.Errorf("ffprobe output exceeds limit")
	}
	return b.Buffer.Write(p)
}

func ProbeVideo(ctx context.Context, path string) (uint64, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-protocol_whitelist", "file,pipe", "-show_entries", "format=format_name,duration:stream=codec_type,codec_name,pix_fmt,width,height", "-of", "json", path)
	var output cappedProbeOutput
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return 0, fmt.Errorf("ffprobe is not installed: %w", err)
		}
		if ctx.Err() != nil {
			return 0, fmt.Errorf("media probe timeout: %w", ctx.Err())
		}
		return 0, ErrUnsupportedVideo
	}
	return validateProbe(output.Bytes())
}

func AttachVideoUpload(ctx context.Context, id, ownerID uint64, filename string, duration uint64) (*model.Video, error) {
	audit := "pending"
	if os.Getenv("VIDEO_UPLOAD_AUTO_APPROVE") == "1" {
		audit = "approved"
	}
	rows, err := repo.AttachVideoMedia(ctx, id, ownerID, filename, fmt.Sprintf("/api/v1/videos/%d/media", id), duration, audit)
	if err != nil {
		return nil, err
	}
	if rows == 0 {
		return nil, ErrVideoNotReady
	}
	// No fallible follow-up read after commit: callers may delete the file on error.
	return &model.Video{VideoID: id, OwnerID: ownerID, DurationMS: duration, PlayURL: fmt.Sprintf("/api/v1/videos/%d/media", id), ProcessStatus: "ready", PublishStatus: "draft", AuditStatus: audit}, nil
}
