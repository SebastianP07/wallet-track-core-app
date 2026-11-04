// Package platform holds infrastructure helpers shared by every module.
package platform

import (
	"fmt"
	"log/slog"
	"time"
)

type Config struct {
	HTTPAddr        string
	LogLevel        slog.Level
	ShutdownTimeout time.Duration
}

// LoadConfig reads the configuration through getenv, normally os.Getenv.
func LoadConfig(getenv func(string) string) (Config, error) {
	cfg := Config{
		HTTPAddr:        ":8080",
		LogLevel:        slog.LevelInfo,
		ShutdownTimeout: 10 * time.Second,
	}

	if addr := getenv("HTTP_ADDR"); addr != "" {
		cfg.HTTPAddr = addr
	}
	if level := getenv("LOG_LEVEL"); level != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(level)); err != nil {
			return Config{}, fmt.Errorf("LOG_LEVEL: %w", err)
		}
	}
	if timeout := getenv("SHUTDOWN_TIMEOUT"); timeout != "" {
		parsed, err := time.ParseDuration(timeout)
		if err != nil {
			return Config{}, fmt.Errorf("SHUTDOWN_TIMEOUT: %w", err)
		}
		if parsed <= 0 {
			return Config{}, fmt.Errorf("SHUTDOWN_TIMEOUT: must be positive, got %s", timeout)
		}
		cfg.ShutdownTimeout = parsed
	}
	return cfg, nil
}
