package config

import "testing"

func TestLoadConfigDecodesDanmakuPolicies(t *testing.T) {
	previous := GlobalConfig
	GlobalConfig = nil
	t.Cleanup(func() { GlobalConfig = previous })

	cfg := LoadConfig()
	if cfg.Danmaku.RecentCache.Size != 1000 || cfg.Danmaku.RecentCache.TTLSeconds != 300 {
		t.Fatalf("unexpected recent cache config: %+v", cfg.Danmaku.RecentCache)
	}
	if !cfg.Danmaku.HotRoom.AdaptiveSamplingEnabled {
		t.Fatal("adaptive sampling should be enabled by config")
	}
	if cfg.Danmaku.HotRoom.MaxFanoutDeliveriesPerSecond != 150000 {
		t.Fatalf("fanout budget=%d", cfg.Danmaku.HotRoom.MaxFanoutDeliveriesPerSecond)
	}
	if cfg.Danmaku.SlowClient.DisconnectStrikes != 100 {
		t.Fatalf("slow-client strikes=%d", cfg.Danmaku.SlowClient.DisconnectStrikes)
	}
	if cfg.Danmaku.Like.MaxPerSecond != 10 {
		t.Fatalf("like max per second=%d", cfg.Danmaku.Like.MaxPerSecond)
	}
	if cfg.Danmaku.Like.ReconcileIntervalSeconds != 30 {
		t.Fatalf("like reconcile interval=%d", cfg.Danmaku.Like.ReconcileIntervalSeconds)
	}
	if cfg.Danmaku.Like.BatchSize != 100 || cfg.Danmaku.Like.BatchWaitMilliseconds != 200 {
		t.Fatalf("unexpected like batch config: %+v", cfg.Danmaku.Like)
	}
	if cfg.Danmaku.Like.IdempotencyTTLSeconds != 86400 || cfg.Danmaku.Like.ReservationTTLSeconds != 60 {
		t.Fatalf("unexpected like idempotency config: %+v", cfg.Danmaku.Like)
	}
}
