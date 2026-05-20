package main

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Username     string
	Password     string
	SMTPHost     string
	SMTPPort     int
	SMTPUser     string
	SMTPPass     string
	NotifyEmail  string
	PollInterval int
}

func LoadConfig() (*Config, error) {
	cfg := &Config{
		Username:     os.Getenv("USST_USERNAME"),
		Password:     os.Getenv("USST_PASSWORD"),
		SMTPHost:     getEnv("SMTP_HOST", "smtp.qq.com"),
		SMTPPort:     getEnvInt("SMTP_PORT", 587),
		SMTPUser:     os.Getenv("SMTP_USER"),
		SMTPPass:     os.Getenv("SMTP_PASS"),
		NotifyEmail:  os.Getenv("NOTIFY_EMAIL"),
		PollInterval: getEnvInt("POLL_INTERVAL", 30),
	}

	var missing []string
	if cfg.Username == "" {
		missing = append(missing, "USST_USERNAME")
	}
	if cfg.Password == "" {
		missing = append(missing, "USST_PASSWORD")
	}
	if cfg.NotifyEmail == "" {
		missing = append(missing, "NOTIFY_EMAIL")
	}
	if cfg.SMTPUser == "" {
		missing = append(missing, "SMTP_USER")
	}
	if cfg.SMTPPass == "" {
		missing = append(missing, "SMTP_PASS")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("缺少必要的环境变量: %v", missing)
	}

	return cfg, nil
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
