package platform_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/SebastianP07/wallet-track-core-app/internal/shared/platform"
)

func TestNewLoggerWritesJSONAtConfiguredLevel(t *testing.T) {
	var out bytes.Buffer
	logger := platform.NewLogger(&out, slog.LevelWarn)

	logger.Info("below the level")
	if out.Len() != 0 {
		t.Fatalf("info record was written at warn level: %q", out.String())
	}

	logger.Warn("at the level", "key", "value")
	var record map[string]any
	if err := json.Unmarshal(out.Bytes(), &record); err != nil {
		t.Fatalf("output is not JSON: %v (%q)", err, out.String())
	}
	if record["msg"] != "at the level" || record["key"] != "value" {
		t.Errorf("unexpected record: %v", record)
	}
}
