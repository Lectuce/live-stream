package model

type KafkaConfig struct {
	Brokers    []string `yaml:"brokers" mapstructure:"brokers"`
	DanmuTopic string   `yaml:"danmu_topic" mapstructure:"danmu_topic"`
	GroupID    string   `yaml:"group_id" mapstructure:"group_id"`
}
