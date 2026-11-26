package middleware

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"live-stream/internal/config"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

var replayAccepted, replayRejected, replayBytes, replayMicros atomic.Uint64
var replayInflight atomic.Int64

func ReplayMetricsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"accepted": replayAccepted.Load(), "rejected": replayRejected.Load(), "inflight": replayInflight.Load(), "response_bytes": replayBytes.Load(), "total_microseconds": replayMicros.Load()})
	})
}

// ReplayLimit bounds both concurrent work and the rate of fast requests.
// Construct once per router, not once per request. No Redis work on rejection.
func ReplayLimit() gin.HandlerFunc {
	cfg := config.ReplaySettings()
	var mu sync.Mutex
	rooms := make(map[uint64]int)
	inflight := 0
	tokens := float64(cfg.Burst)
	previous := time.Now()
	return func(c *gin.Context) {
		room, err := strconv.ParseUint(c.Param("room_id"), 10, 64)
		if err != nil {
			c.AbortWithStatusJSON(400, gin.H{"message": "room id格式错误"})
			return
		}
		mu.Lock()
		now := time.Now()
		tokens += now.Sub(previous).Seconds() * float64(cfg.RequestsPerSecond)
		previous = now
		if tokens > float64(cfg.Burst) {
			tokens = float64(cfg.Burst)
		}
		allowed := inflight < cfg.MaxInflight && rooms[room] < cfg.MaxInflightPerRoom && (len(rooms) < 1024 || rooms[room] > 0) && tokens >= 1
		if allowed {
			tokens--
			inflight++
			rooms[room]++
		}
		mu.Unlock()
		if !allowed {
			replayRejected.Add(1)
			c.Header("Retry-After", "1")
			c.AbortWithStatusJSON(429, gin.H{"message": "消息同步繁忙，请稍后重试", "code": "replay_busy"})
			return
		}
		replayAccepted.Add(1)
		replayInflight.Add(1)
		started := time.Now()
		defer func() {
			replayInflight.Add(-1)
			replayMicros.Add(uint64(time.Since(started).Microseconds()))
			if n := c.Writer.Size(); n > 0 {
				replayBytes.Add(uint64(n))
			}
			mu.Lock()
			inflight--
			rooms[room]--
			if rooms[room] == 0 {
				delete(rooms, room)
			}
			mu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(c.Request.Context(), time.Duration(cfg.TimeoutMilliseconds)*time.Millisecond)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
