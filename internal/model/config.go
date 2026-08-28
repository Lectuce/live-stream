package model

type Config struct {
	Mysql      MySQLConfig      `yaml:"mysql" mapstructure:"mysql"`
	Redis      RedisConfig      `yaml:"redis" mapstructure:"redis"`
	Kafka      KafkaConfig      `yaml:"kafka" mapstructure:"kafka"`
	StreamList StreamListConfig `yaml:"stream_list" mapstructure:"stream_list"`
}
