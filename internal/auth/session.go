package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"live-stream/internal/dao"
	"time"
)

const SessionTTL = 7 * 24 * time.Hour

// GenSessionToken 生成随机session token（hex字符串，作为cookie里sid）
func GenSessionToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// StoreSession 将token哈希后存入redis，绑定uid
func StoreSession(ctx context.Context, token string, uid uint64) error {
	h := sha256.Sum256([]byte(token))
	key := "session:" + hex.EncodeToString(h[:])
	return dao.SetSession(ctx, key, uid, SessionTTL)
}

// GetSessionByToken 根据sid校验，获取用户uid
func GetSessionByToken(ctx context.Context, token string) (uint64, error) {
	h := sha256.Sum256([]byte(token))
	key := "session:" + hex.EncodeToString(h[:])
	val, err := dao.GetSession(ctx, key)
	if err != nil {
		return 0, err
	}
	return val, nil
}

// DelSession 登出，删除redis里的会话
func DelSession(ctx context.Context, token string) error {
	h := sha256.Sum256([]byte(token))
	key := "session:" + hex.EncodeToString(h[:])
	return dao.DelSession(ctx, key)
}
