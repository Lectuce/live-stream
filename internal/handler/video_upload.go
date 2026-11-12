package handler

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"live-stream/internal/config"
	"live-stream/internal/middleware"
	"live-stream/internal/service"
)

const maxVideoBytes int64 = 100 << 20

var videoUploadSlot = make(chan struct{}, 1)

func trustedUploadOrigin(c *gin.Context) bool {
	origin := c.GetHeader("Origin")
	if origin == "" {
		return true
	} // command-line clients still require authentication
	u, err := url.Parse(origin)
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host == c.Request.Host {
		return true
	}
	if config.GlobalConfig != nil {
		for _, allowed := range config.GlobalConfig.Server.AllowedOrigins {
			if origin == allowed {
				return true
			}
		}
	}
	return false
}

func UploadVideo(c *gin.Context) {
	if !trustedUploadOrigin(c) {
		c.JSON(403, gin.H{"message": "上传来源不允许"})
		return
	}
	uid, ok := middleware.GetUserID(c)
	if !ok {
		c.JSON(401, gin.H{"message": "未登录"})
		return
	}
	id, ok := parseVideoID(c)
	if !ok {
		return
	}
	if _, err := service.VideoUploadDraft(c.Request.Context(), id, uid); err != nil {
		videoError(c, err)
		return
	}
	select {
	case videoUploadSlot <- struct{}{}:
		defer func() { <-videoUploadSlot }()
	default:
		c.Header("Retry-After", "5")
		c.JSON(429, gin.H{"message": "另一个视频正在上传，请稍后重试"})
		return
	}
	controller := http.NewResponseController(c.Writer)
	_ = controller.SetReadDeadline(time.Now().Add(3 * time.Minute))
	defer func() { _ = controller.SetReadDeadline(time.Time{}) }()
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxVideoBytes+(1<<20))
	reader, err := c.Request.MultipartReader()
	if err != nil {
		c.JSON(400, gin.H{"message": "请使用 multipart/form-data 上传"})
		return
	}
	dir, err := service.VideoStorageDir()
	if err != nil {
		videoError(c, err)
		return
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		videoError(c, err)
		return
	}
	temp, err := os.CreateTemp(dir, "upload-*.part")
	if err != nil {
		videoError(c, err)
		return
	}
	tempPath := temp.Name()
	defer func() { _ = temp.Close(); _ = os.Remove(tempPath) }()
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "file" || strings.ToLower(filepath.Ext(part.FileName())) != ".mp4" {
		c.JSON(400, gin.H{"message": "只接受一个名为 file 的 MP4 文件字段"})
		return
	}
	n, err := io.Copy(temp, io.LimitReader(part, maxVideoBytes+1))
	if n > maxVideoBytes {
		c.JSON(413, gin.H{"message": "视频不能超过 100 MiB"})
		return
	}
	if err != nil || n == 0 {
		c.JSON(400, gin.H{"message": "文件为空或上传中断"})
		return
	}
	if _, err = reader.NextPart(); err != io.EOF {
		c.JSON(400, gin.H{"message": "仅允许一个文件字段，或上传数据不完整"})
		return
	}
	if err = temp.Close(); err != nil {
		videoError(c, err)
		return
	}
	// Reject non-MP4 content even when renamed to .mp4.
	f, err := os.Open(tempPath)
	if err != nil {
		videoError(c, err)
		return
	}
	header := make([]byte, 12)
	_, err = io.ReadFull(f, header)
	_ = f.Close()
	if err != nil || string(header[4:8]) != "ftyp" {
		c.JSON(415, gin.H{"message": "文件内容不是支持的 MP4"})
		return
	}
	duration, err := service.ProbeVideo(c.Request.Context(), tempPath)
	if errors.Is(err, service.ErrUnsupportedVideo) {
		c.JSON(415, gin.H{"message": "需要 H.264/yuv420p 视频、可选 AAC 音频的 MP4，最高1080p、最长1小时"})
		return
	}
	if err != nil {
		videoError(c, err)
		return
	}
	filename := uuid.NewString() + ".mp4"
	finalPath := filepath.Join(dir, filename)
	if err = os.Rename(tempPath, finalPath); err != nil {
		videoError(c, err)
		return
	}
	video, err := service.AttachVideoUpload(c.Request.Context(), id, uid, filename, duration)
	if err != nil {
		_ = os.Remove(finalPath)
		videoError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": video, "message": "上传成功"})
}

func ServeVideoMedia(c *gin.Context) {
	id, ok := parseVideoID(c)
	if !ok {
		return
	}
	c.Header("Cache-Control", "private, no-store")
	video, err := service.GetPublicVideo(c.Request.Context(), id)
	if err != nil {
		videoError(c, err)
		return
	}
	name := video.MediaFile
	if name == "" || filepath.Base(name) != name || filepath.Ext(name) != ".mp4" {
		c.Status(404)
		return
	}
	dir, err := service.VideoStorageDir()
	if err != nil {
		videoError(c, err)
		return
	}
	f, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		c.Status(404)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		c.Status(404)
		return
	}
	c.Header("Content-Type", "video/mp4")
	c.Header("X-Content-Type-Options", "nosniff")
	http.ServeContent(c.Writer, c.Request, name, info.ModTime(), f)
}
