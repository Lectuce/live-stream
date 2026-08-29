package model

import (
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type StorageContainer struct {
	Db    *gorm.DB
	Redis *redis.Client
}
