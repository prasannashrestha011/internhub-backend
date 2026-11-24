package config

import (
	"fmt"
	"net/mail"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	App      AppConfig
	Database DatabaseConfig
	JWT      JWTConfig
	MinIO    MinIOConfig
	Server   ServerConfig
	Email    EmailConfig
}

type EmailConfig struct {
	Host      string
	Port      string
	Username  string
	Password  string
	From      string
	OTPSecret string
}

func (e EmailConfig) Validate() error {
	if e.Host == "" || strings.ContainsAny(e.Host, "\r\n /:") || e.Username == "" || e.Password == "" {
		return fmt.Errorf("SMTP_HOST, SMTP_USERNAME and SMTP_PASSWORD are required")
	}
	if e.Port != "465" && e.Port != "587" {
		return fmt.Errorf("SMTP_PORT must be 465 (TLS) or 587 (STARTTLS)")
	}
	address, err := mail.ParseAddress(e.From)
	if err != nil || address.Address != e.From || strings.ContainsAny(e.From, "\r\n") {
		return fmt.Errorf("SMTP_FROM must be a plain approved sender email address")
	}
	if len(e.OTPSecret) < 32 {
		return fmt.Errorf("EMAIL_OTP_SECRET must contain at least 32 characters")
	}
	return nil
}

type AppConfig struct {
	Env  string
	Port string
}

type DatabaseConfig struct {
	Host     string
	Port     string
	Name     string
	User     string
	Password string
	SSLMode  string
}

type JWTConfig struct {
	AccessSecret  string
	AccessExpiry  time.Duration
	RefreshExpiry time.Duration
}

type MinIOConfig struct {
	Endpoint         string
	AccessKey        string
	SecretKey        string
	UseSSL           bool
	ProfileBucket    string
	StudentDocBucket string
}

type ServerConfig struct {
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

func Load() (*Config, error) {
	// Load .env file if it exists (optional for development)
	_ = godotenv.Load()

	config := &Config{
		App: AppConfig{
			Env:  getEnv("APP_ENV", "development"),
			Port: getEnv("APP_PORT", "8080"),
		},
		Database: DatabaseConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnv("DB_PORT", "5432"),
			Name:     getEnv("DB_NAME", "student_job_portal"),
			User:     getEnv("DB_USER", "postgres"),
			Password: getEnv("DB_PASSWORD", "postgres"),
			SSLMode:  getEnv("DB_SSL_MODE", "disable"),
		},
		JWT: JWTConfig{
			AccessSecret:  getEnv("JWT_ACCESS_SECRET", ""),
			AccessExpiry:  parseDuration(getEnv("JWT_ACCESS_EXPIRY", "15m")),
			RefreshExpiry: parseDuration(getEnv("JWT_REFRESH_EXPIRY", "168h")),
		},
		MinIO: MinIOConfig{
			Endpoint:         getEnv("MINIO_ENDPOINT", "localhost:9000"),
			AccessKey:        getEnv("MINIO_ACCESS_KEY", ""),
			SecretKey:        getEnv("MINIO_SECRET_KEY", ""),
			UseSSL:           getEnv("MINIO_USE_SSL", "false") == "true",
			ProfileBucket:    getEnv("MINIO_PROFILE_BUCKET", "profile-images"),
			StudentDocBucket: getEnv("MINIO_STUDENT_DOCUMENT_BUCKET", "student-documents"),
		},
		Server: ServerConfig{
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
		},
		Email: EmailConfig{
			Host:      getEnv("SMTP_HOST", ""),
			Port:      getEnv("SMTP_PORT", "587"),
			Username:  getEnv("SMTP_USERNAME", ""),
			Password:  getEnv("SMTP_PASSWORD", ""),
			From:      getEnv("SMTP_FROM", ""),
			OTPSecret: getEnv("EMAIL_OTP_SECRET", ""),
		},
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	return config, nil
}

func (c *Config) Validate() error {
	if c.App.Env == "" {
		return fmt.Errorf("APP_ENV is required")
	}
	if c.Database.Host == "" {
		return fmt.Errorf("DB_HOST is required")
	}
	if c.Database.Name == "" {
		return fmt.Errorf("DB_NAME is required")
	}
	if c.JWT.AccessSecret == "" {
		return fmt.Errorf("JWT_ACCESS_SECRET is required")
	}
	if c.MinIO.AccessKey == "" {
		return fmt.Errorf("MINIO_ACCESS_KEY is required")
	}
	if c.MinIO.SecretKey == "" {
		return fmt.Errorf("MINIO_SECRET_KEY is required")
	}
	if c.App.Env == "production" || c.Email.Host != "" || c.Email.Username != "" || c.Email.Password != "" || c.Email.From != "" || c.Email.OTPSecret != "" {
		if err := c.Email.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) GetDSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		c.Database.Host,
		c.Database.Port,
		c.Database.User,
		c.Database.Password,
		c.Database.Name,
		c.Database.SSLMode,
	)
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func parseDuration(value string) time.Duration {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return time.Duration(0)
	}
	return duration
}
