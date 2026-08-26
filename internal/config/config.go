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
