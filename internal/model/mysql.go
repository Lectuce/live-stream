package model

import (
	"fmt"
	"net"

	"github.com/go-sql-driver/mysql"
)

type MySQLConfig struct {
	Host     string `yaml:"host" mapstructure:"host"`
	Port     int    `yaml:"port" mapstructure:"port"`
	User     string `yaml:"user" mapstructure:"user"`
	Password string `yaml:"password" mapstructure:"password"`
	DBName   string `yaml:"dbname" mapstructure:"dbname"`
	Charset  string `yaml:"charset" mapstructure:"charset"`
}

func (m *MySQLConfig) DSN() string {
	addr := m.Host
	if ip := net.ParseIP(m.Host); ip != nil && ip.To4() == nil {
		addr = fmt.Sprintf("[%s]", m.Host)
	}
	cfg := mysql.Config{
		User:   m.User,
		Passwd: m.Password,
		Net:    "tcp",
		Addr:   fmt.Sprintf("%s:%d", addr, m.Port),
		DBName: m.DBName,
		Params: map[string]string{
			"charset":   m.Charset,
			"parseTime": "True",
			"loc":       "Local",
		},
	}
	return cfg.FormatDSN()
}
