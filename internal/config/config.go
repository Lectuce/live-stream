package config

import (
	"fmt"
	"live-stream/internal/model"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

var GlobalConfig *model.Config

// 寻找go mod根目录
func findProjectRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		panic(fmt.Errorf("can not find go.mod: %v", err))
	}
	for {
		modPath := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(modPath); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic(fmt.Errorf("can not find go.mod: %v", err))
		}
		dir = parent
	}
}

func LoadConfig() *model.Config {
	if GlobalConfig != nil {
		return GlobalConfig
	}
	root := findProjectRoot()
	configPath := filepath.Join(root, "config", "config.yaml")
	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetDefault("danmaku.recent_cache.size", 1000)
	v.SetDefault("danmaku.recent_cache.ttl_seconds", 300)
	v.SetDefault("danmaku.hot_room.adaptive_sampling_enabled", true)
	v.SetDefault("danmaku.hot_room.online_threshold", 5000)
	v.SetDefault("danmaku.hot_room.max_fanout_deliveries_per_second", 150000)
	v.SetDefault("danmaku.hot_room.max_sample_stride", 16)
	v.SetDefault("danmaku.hot_room.presence_interval_milliseconds", 1000)
	v.SetDefault("danmaku.slow_client.queue_high_watermark_percent", 75)
	v.SetDefault("danmaku.slow_client.disconnect_strikes", 100)
	v.SetDefault("danmaku.slow_client.disconnect_grace_milliseconds", 5000)
	err := v.ReadInConfig()
	if err != nil {
		panic(fmt.Errorf("fatal error config file: %w", err))
	}
	var cfg model.Config
	err = v.Unmarshal(&cfg)
	if err != nil {
		panic(fmt.Errorf("unable to decode into struct, %v", err))
	}
	GlobalConfig = &cfg
	return GlobalConfig
}
