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
}
