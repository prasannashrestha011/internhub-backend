package config

import (
	"strings"
	"testing"
	"time"
)

func productionConfig() Config {
	return Config{
		App:      AppConfig{Env: "production", AllowedOrigins: []string{"https://portal.example.com"}, TrustedProxies: []string{"172.30.0.2"}},
		Database: DatabaseConfig{Host: "postgres", Name: "portal", Password: "random-database-password"},
		JWT:      JWTConfig{AccessSecret: strings.Repeat("a", 32), AccessExpiry: time.Minute, RefreshExpiry: time.Hour},
		MinIO:    MinIOConfig{PublicURL: "https://files.example.com", AccessKey: "storage-user", SecretKey: "random-storage-secret"},
		Email:    EmailConfig{Host: "smtp.example.com", Port: "587", Username: "user", Password: "secret", From: "no-reply@example.com", OTPSecret: strings.Repeat("b", 32)},
	}
}

func TestProductionConfigurationRejectsUnsafeSettings(t *testing.T) {
	valid := productionConfig()
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Config){
		"local CORS":                func(c *Config) { c.App.AllowedOrigins = []string{"http://localhost:3000"} },
		"wildcard CORS":             func(c *Config) { c.App.AllowedOrigins = []string{"https://*.example.com"} },
		"empty CORS":                func(c *Config) { c.App.AllowedOrigins = nil },
		"all proxies":               func(c *Config) { c.App.TrustedProxies = []string{"0.0.0.0/0"} },
		"invalid proxy":             func(c *Config) { c.App.TrustedProxies = []string{"caddy"} },
		"cookie outside frontend":   func(c *Config) { c.App.CookieDomain = "attacker.example" },
		"short JWT key":             func(c *Config) { c.JWT.AccessSecret = "short" },
		"default database password": func(c *Config) { c.Database.Password = "postgres" },
		"default storage password":  func(c *Config) { c.MinIO.SecretKey = "minioadmin" },
		"missing public storage":    func(c *Config) { c.MinIO.PublicURL = "" },
		"insecure public storage":   func(c *Config) { c.MinIO.PublicURL = "http://files.example.com" },
		"storage path":              func(c *Config) { c.MinIO.PublicURL = "https://files.example.com/s3" },
		"invalid access duration":   func(c *Config) { c.JWT.AccessExpiry = parseDuration("invalid") },
		"negative refresh duration": func(c *Config) { c.JWT.RefreshExpiry = -time.Hour },
	} {
		t.Run(name, func(t *testing.T) {
			c := productionConfig()
			mutate(&c)
			if err := c.Validate(); err == nil {
				t.Fatal("unsafe configuration accepted")
			}
		})
	}
}
