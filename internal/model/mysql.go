package model

import (
	"fmt"

	"github.com/go-sql-driver/mysql"
)

type MySQLConfig struct {
	Host     string `yaml:"host" mapstructure:"host"`
	Port     int    `yaml:"port mapstructure:"port"`
	User     string `yaml:"user" mapstructure:"user"`
	Password string `yaml:"password" mapstructure:"password"`
	DBName   string `yaml:"dbname" mapstructure:"dbname"`
	Charset  string `yaml:"charset" mapstructure:"charset"`
}

func (m *MySQLConfig) DSN() string {
	cfg := mysql.Config{
		User:   m.User,
		Passwd: m.Password,
		Net:    "tcp",
		Addr:   fmt.Sprintf("%s:%d", m.Host, m.Port),
		DBName: m.DBName,
		Params: map[string]string{
			"charset":   m.Charset,
			"parseTime": "True",
			"loc":       "Local",
		},
	}
	return cfg.FormatDSN()
}
