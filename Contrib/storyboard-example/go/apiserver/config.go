package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Models struct {
	Chat  string
	Image string
	Video string
}

type Config struct {
	ProfileFile string
	Host        string
	Port        int
	Models      Models
}

func loadConfig() (Config, error) {
	port, err := strconv.Atoi(envOrDefault("API_PORT", "8080"))
	if err != nil || port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("API_PORT must be an integer between 1 and 65535")
	}

	cfg := Config{
		ProfileFile: envOrDefault("PROFILE_FILE", "profile.json"),
		Host:        envOrDefault("API_HOST", "127.0.0.1"),
		Port:        port,
		Models: Models{
			Chat:  envOrDefault("STORYBOARD_CHAT_MODEL", "deepseek-v4-pro"),
			Image: envOrDefault("STORYBOARD_IMAGE_MODEL", "z-image-turbo"),
			Video: envOrDefault("STORYBOARD_VIDEO_MODEL", "kling-3.0"),
		},
	}
	return cfg, nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
