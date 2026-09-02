package model

type ServerConfig struct {
	AllowedOrigins []string `yaml:"allowed_origins" mapstructure:"allowed_origins"`
	CookieSecure   bool     `yaml:"cookie_secure" mapstructure:"cookie_secure"`
	CookieSameSite string   `yaml:"cookie_same_site" mapstructure:"cookie_same_site"`
}
