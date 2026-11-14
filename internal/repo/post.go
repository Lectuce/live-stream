package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"live-stream/internal/dao"
	"live-stream/internal/model"
)

var ErrPostConflict = errors.New("post changed; reload")
var ErrPostImages = errors.New("invalid post images")
var ErrEmptyPost = errors.New("post requires content or images")

func CreatePost(ctx context.Context, owner uint64) (*model.PostView, error) {
	post := model.Post{OwnerID: owner, Status: "draft", Version: 1}
	if err := dao.SC.Db.WithContext(ctx).Create(&post).Error; err != nil {
		return nil, err
	}
	return &model.PostView{Post: post, Images: []model.PostImageView{}}, nil
}

func PostImages(ctx context.Context, ids []uint64) (map[uint64][]model.PostImageView, error) {
	result := make(map[uint64][]model.PostImageView)
	if len(ids) == 0 {
		return result, nil
	}
	var images []model.PostImage
	if err := dao.SC.Db.WithContext(ctx).Where("post_id IN ?", ids).Order("position ASC, id ASC").Find(&images).Error; err != nil {
		return nil, err
	}
	for _, img := range images {
		result[img.PostID] = append(result[img.PostID], model.PostImageView{ID: img.ID, URL: "/api/v1/posts/images/" + img.ID})
	}
	return result, nil
}

func GetPost(ctx context.Context, id, owner uint64) (*model.PostView, error) {
	var post model.Post
	query := dao.SC.Db.WithContext(ctx).Where("post_id = ?", id)
	if owner == 0 {
		query = query.Where("status = 'published'")
	} else {
		query = query.Where("owner_id = ?", owner)
	}
	if err := query.First(&post).Error; err != nil {
		return nil, err
	}
	images, err := PostImages(ctx, []uint64{id})
	if err != nil {
		return nil, err
	}
	list := images[id]
	if list == nil {
		list = []model.PostImageView{}
	}
	return &model.PostView{Post: post, Images: list}, nil
}

func ListPosts(ctx context.Context, owner, before uint64, limit int, public bool) ([]model.PostView, error) {
	var posts []model.Post
	query := dao.SC.Db.WithContext(ctx).Where("owner_id = ?", owner)
	if public {
		query = query.Where("status = 'published'")
	}
	if before > 0 {
		query = query.Where("post_id < ?", before)
	}
	if err := query.Order("post_id DESC").Limit(limit).Find(&posts).Error; err != nil {
		return nil, err
	}
	ids := make([]uint64, 0, len(posts))
	for _, p := range posts {
		ids = append(ids, p.PostID)
	}
	images, err := PostImages(ctx, ids)
	if err != nil {
		return nil, err
	}
	result := make([]model.PostView, 0, len(posts))
	for _, p := range posts {
		list := images[p.PostID]
		if list == nil {
			list = []model.PostImageView{}
		}
		result = append(result, model.PostView{Post: p, Images: list})
	}
	return result, nil
}

func EditPost(ctx context.Context, id, owner uint64, req model.EditPostRequest) error {
	return dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var post model.Post
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("post_id = ? AND owner_id = ?", id, owner).First(&post).Error; err != nil {
			return err
		}
		if post.Version != req.Version {
			return ErrPostConflict
		}
		if post.Status == "published" && req.Content == "" && len(req.ImageIDs) == 0 {
			return ErrEmptyPost
		}
		var images []model.PostImage
		if len(req.ImageIDs) > 0 {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", req.ImageIDs).Order("id ASC").Find(&images).Error; err != nil {
				return err
			}
			if len(images) != len(req.ImageIDs) {
				return ErrPostImages
			}
			for _, img := range images {
				if img.OwnerID != owner || (img.PostID != 0 && img.PostID != id) {
					return ErrPostImages
				}
			}
		}
		if err := tx.Model(&model.PostImage{}).Where("post_id = ?", id).Updates(map[string]any{"post_id": 0, "position": 0}).Error; err != nil {
			return err
		}
		for i, imgID := range req.ImageIDs {
			if err := tx.Model(&model.PostImage{}).Where("id = ?", imgID).Updates(map[string]any{"post_id": id, "position": i}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&post).Updates(map[string]any{"content": req.Content, "version": gorm.Expr("version + 1")}).Error
	})
}

func PublishPost(ctx context.Context, id, owner, version uint64) error {
	return dao.SC.Db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var post model.Post
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("post_id = ? AND owner_id = ?", id, owner).First(&post).Error; err != nil {
			return err
		}
		if post.Version != version {
			return ErrPostConflict
		}
		var count int64
		if err := tx.Model(&model.PostImage{}).Where("post_id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if post.Content == "" && count == 0 {
			return ErrEmptyPost
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		if post.PublishedAt != nil {
			now = *post.PublishedAt
		}
		if post.Status != "published" {
			if err := tx.Model(&post).Updates(map[string]any{"status": "published", "published_at": now}).Error; err != nil {
				return err
			}
		}
		return QueueContentEvent(tx, "post.published", owner, id, now)
	})
}

func DeletePost(ctx context.Context, id, owner uint64) error {
	result := dao.SC.Db.WithContext(ctx).Where("post_id = ? AND owner_id = ?", id, owner).Delete(&model.Post{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func GetPostImage(ctx context.Context, id string, viewer uint64) (*model.PostImage, error) {
	var image model.PostImage
	if err := dao.SC.Db.WithContext(ctx).Where("id = ?", id).First(&image).Error; err != nil {
		return nil, err
	}
	if image.PostID == 0 {
		if viewer != image.OwnerID {
			return nil, gorm.ErrRecordNotFound
		}
	} else {
		var count int64
		if err := dao.SC.Db.WithContext(ctx).Model(&model.Post{}).Where("post_id = ? AND (status = 'published' OR owner_id = ?)", image.PostID, viewer).Count(&count).Error; err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, gorm.ErrRecordNotFound
		}
	}
	if image.Filename != fmt.Sprintf("%s.jpg", image.ID) {
		return nil, errors.New("invalid stored image filename")
	}
	return &image, nil
}
