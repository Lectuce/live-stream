package dao

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var communityLimitScript = redis.NewScript(`
for i,key in ipairs(KEYS) do
 if tonumber(redis.call('GET',key) or '0') >= tonumber(ARGV[i]) then return 0 end
end
for _,key in ipairs(KEYS) do
 local n=redis.call('INCR',key)
 if n==1 then redis.call('EXPIRE',key,60) end
end
return 1`)

func CommunityLimitPolicy(action string) (int, int, int) {
	account := 120
	switch action {
	case "comment":
		account = 40
	case "follow":
		account = 60
	case "report":
		account = 10
	case "danmaku":
		account = 120
	}
	if value, err := strconv.Atoi(os.Getenv("COMMUNITY_LIMIT_" + strings.ToUpper(action))); err == nil && value >= 1 && value <= 10000 {
		account = value
	}
	return account, account * 5, max(1, account/2)
}

// One fixed 60-second window per account, trusted client IP and account/target pair.
func AllowCommunityWrite(ctx context.Context, action string, actor uint64, ip, target string) (bool, error) {
	if SC == nil || SC.Redis == nil {
		return false, errors.New("rate limiter unavailable")
	}
	account, network, pair := CommunityLimitPolicy(action)
	keys := []string{fmt.Sprintf("community:rate:%s:u:%d", action, actor), fmt.Sprintf("community:rate:%s:ip:%x", action, sha256.Sum256([]byte(ip))), fmt.Sprintf("community:rate:%s:ut:%d:%x", action, actor, sha256.Sum256([]byte(target)))}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	n, err := communityLimitScript.Run(ctx, SC.Redis, keys, account, network, pair).Int()
	return n == 1, err
}
