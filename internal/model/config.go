package model

type Config struct {
	Mysql      MySQLConfig      `yaml:"mysql" mapstructure:"mysql"`
	Redis      RedisConfig      `yaml:"redis" mapstructure:"redis"`
	Kafka      KafkaConfig      `yaml:"kafka" mapstructure:"kafka"`
	StreamList StreamListConfig `yaml:"stream_list" mapstructure:"stream_list"`
	Server     ServerConfig     `yaml:"server" mapstructure:"server"`
	Danmaku    DanmakuConfig    `yaml:"danmaku" mapstructure:"danmaku"`
}

type DanmakuConfig struct {
	RecentCache RecentCacheConfig `yaml:"recent_cache" mapstructure:"recent_cache"`
	HotRoom     HotRoomConfig     `yaml:"hot_room" mapstructure:"hot_room"`
	SlowClient  SlowClientConfig  `yaml:"slow_client" mapstructure:"slow_client"`
	Like        LikeConfig        `yaml:"like" mapstructure:"like"`
}

type LikeConfig struct {
	MaxPerSecond             int `yaml:"max_per_second" mapstructure:"max_per_second"`
	ReconcileIntervalSeconds int `yaml:"reconcile_interval_seconds" mapstructure:"reconcile_interval_seconds"`
}

type RecentCacheConfig struct {
	Size       int `yaml:"size" mapstructure:"size"`
	TTLSeconds int `yaml:"ttl_seconds" mapstructure:"ttl_seconds"`
}

type HotRoomConfig struct {
	AdaptiveSamplingEnabled      bool  `yaml:"adaptive_sampling_enabled" mapstructure:"adaptive_sampling_enabled"`
	OnlineThreshold              int   `yaml:"online_threshold" mapstructure:"online_threshold"`
	MaxFanoutDeliveriesPerSecond int64 `yaml:"max_fanout_deliveries_per_second" mapstructure:"max_fanout_deliveries_per_second"`
	MaxSampleStride              int   `yaml:"max_sample_stride" mapstructure:"max_sample_stride"`
	PresenceIntervalMilliseconds int   `yaml:"presence_interval_milliseconds" mapstructure:"presence_interval_milliseconds"`
}

type SlowClientConfig struct {
	QueueHighWatermarkPercent   int `yaml:"queue_high_watermark_percent" mapstructure:"queue_high_watermark_percent"`
	DisconnectStrikes           int `yaml:"disconnect_strikes" mapstructure:"disconnect_strikes"`
	DisconnectGraceMilliseconds int `yaml:"disconnect_grace_milliseconds" mapstructure:"disconnect_grace_milliseconds"`
}
