package handler

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"live-stream/internal/auth"
	"live-stream/internal/dao"
	"live-stream/internal/model"
	"live-stream/internal/repo"
)

var postImageSlot = make(chan struct{}, 1)

func postImageDir() string {
	if dir := os.Getenv("POST_IMAGE_STORAGE_DIR"); dir != "" {
		return dir
	}
	return filepath.Join("storage", "post-images")
}

// Decode and re-encode: no SVG, animated images, client filenames or embedded metadata.
func normalizePostImage(data []byte) ([]byte, error) {
	if len(data) > 2<<20 {
		return nil, errors.New("图片不能超过2MiB")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") {
		return nil, errors.New("仅支持有效的JPEG和PNG图片")
	}
	if config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 || int64(config.Width)*int64(config.Height) > 12_000_000 {
		return nil, errors.New("图片尺寸过大，边长最多4096且总像素不超过1200万")
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("图片解码失败")
	}
	canvas := image.NewRGBA(source.Bounds())
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(canvas, canvas.Bounds(), source, source.Bounds().Min, draw.Over)
	var output bytes.Buffer
	if err := jpeg.Encode(&output, canvas, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	if output.Len() > 5<<20 {
		return nil, errors.New("处理后的图片过大")
	}
	return output.Bytes(), nil
}

func UploadPostImage(c *gin.Context) {
	uid, ok := postIdentity(c)
	if !ok {
		return
	}
	select {
	case postImageSlot <- struct{}{}:
		defer func() { <-postImageSlot }()
	default:
		c.Header("Retry-After", "2")
		c.JSON(429, gin.H{"message": "图片正在处理，请稍后重试"})
		return
	}
	controller := http.NewResponseController(c.Writer)
	_ = controller.SetReadDeadline(time.Now().Add(time.Minute))
	defer controller.SetReadDeadline(time.Time{})
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 3<<20)
	reader, err := c.Request.MultipartReader()
	if err != nil {
		c.JSON(400, gin.H{"message": "请使用multipart上传图片"})
		return
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "file" {
		c.JSON(400, gin.H{"message": "需要file图片字段"})
		return
	}
	data, err := io.ReadAll(io.LimitReader(part, (2<<20)+1))
	part.Close()
	if err != nil {
		c.JSON(400, gin.H{"message": "图片读取失败"})
		return
	}
	if len(data) > 2<<20 {
		c.JSON(413, gin.H{"message": "图片不能超过2MiB"})
		return
	}
	if _, err = reader.NextPart(); err != io.EOF {
		c.JSON(400, gin.H{"message": "每次仅上传一张图片"})
		return
	}
	encoded, err := normalizePostImage(data)
	if err != nil {
		c.JSON(400, gin.H{"message": err.Error()})
		return
	}
	id := uuid.NewString()
	filename := id + ".jpg"
	dir := postImageDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		postError(c, err)
		return
	}
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		postError(c, err)
		return
	}
	if err := dao.SC.Db.WithContext(c.Request.Context()).Create(&model.PostImage{ID: id, OwnerID: uid, Filename: filename}).Error; err != nil {
		_ = os.Remove(path)
		postError(c, err)
		return
	}
	c.JSON(201, gin.H{"data": model.PostImageView{ID: id, URL: "/api/v1/posts/images/" + id}, "message": "图片已上传"})
}

func ServePostImage(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	id := c.Param("image_id")
	if _, err := uuid.Parse(id); err != nil {
		c.Status(404)
		return
	}
	var viewer uint64
	if token, err := c.Cookie("sid"); err == nil {
		viewer, _ = auth.GetSessionByToken(c.Request.Context(), token)
	}
	img, err := repo.GetPostImage(c.Request.Context(), id, viewer)
	if err != nil {
		postError(c, err)
		return
	}
	file, err := os.Open(filepath.Join(postImageDir(), img.Filename))
	if err != nil {
		c.Status(404)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		postError(c, err)
		return
	}
	c.Header("Content-Type", "image/jpeg")
	c.Header("X-Content-Type-Options", "nosniff")
	http.ServeContent(c.Writer, c.Request, img.Filename, info.ModTime(), file)
}
