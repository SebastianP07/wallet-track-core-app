package platform_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/SebastianP07/wallet-track-core-app/internal/shared/platform"
)

func TestLoadConfig(t *testing.T) {
	defaults := platform.Config{
		HTTPAddr:        ":8080",
		LogLevel:        slog.LevelInfo,
		ShutdownTimeout: 10 * time.Second,
	}

	tests := []struct {
		name    string
		env     map[string]string
		want    platform.Config
		wantErr bool
	}{
		{name: "empty environment uses defaults", want: defaults},
		{
			name: "environment overrides defaults",
			env:  map[string]string{"HTTP_ADDR": "127.0.0.1:9000", "LOG_LEVEL": "warn", "SHUTDOWN_TIMEOUT": "2s"},
			want: platform.Config{HTTPAddr: "127.0.0.1:9000", LogLevel: slog.LevelWarn, ShutdownTimeout: 2 * time.Second},
		},
		{name: "unknown log level is rejected", env: map[string]string{"LOG_LEVEL": "loud"}, wantErr: true},
		{name: "malformed timeout is rejected", env: map[string]string{"SHUTDOWN_TIMEOUT": "soon"}, wantErr: true},
		{name: "non-positive timeout is rejected", env: map[string]string{"SHUTDOWN_TIMEOUT": "0s"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := platform.LoadConfig(func(key string) string { return tt.env[key] })
			if tt.wantErr {
				if err == nil {
					t.Fatalf("LoadConfig() = %+v, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadConfig() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("LoadConfig() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
