package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/mcp-server-recall/internal/config"
)

func TestReadDashboardSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer os.Unsetenv("XDG_CONFIG_HOME")

	// Ensure config directory exists and write a dummy config to override DBPath
	cfgDir := filepath.Join(tmpDir, "mcp-server-recall")
	os.MkdirAll(cfgDir, 0755)
	configPath := filepath.Join(cfgDir, "recall.yaml")
	os.WriteFile(configPath, []byte("dbPath: "+tmpDir+"\n"), 0644)

	Cfg = config.New("1.0")
	// We can just write a file there if we know where it is, or we can just let it fail.

	// If it fails, it returns a dummy log
	snap, logs, err := ReadDashboardSnapshot()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(logs) != 1 || logs[0].Msg != "Awaiting daemon telemetry sync..." {
		t.Errorf("expected dummy log, got %v", logs)
	}
	if snap == nil {
		t.Errorf("expected non-nil snapshot map")
	}

	// Now try with a real file. To do this, we need to know Cfg.GetDBPath()
	dbPath := Cfg.GetDBPath()
	if err := os.MkdirAll(dbPath, 0755); err == nil {
		content := `{"key":"value"}
{"level":"info","msg":"test log"}
`
		err = os.WriteFile(filepath.Join(dbPath, "telemetry.ring"), []byte(content), 0644)
		if err == nil {
			snap2, logs2, err2 := ReadDashboardSnapshot()
			if err2 != nil {
				t.Fatalf("expected no error, got %v", err2)
			}
			if snap2["key"] != "value" {
				t.Errorf("expected key=value, got %v", snap2)
			}
			if len(logs2) != 1 || logs2[0].Msg != "test log" {
				t.Errorf("expected test log, got %v", logs2)
			}

			// Cleanup
			os.Remove(filepath.Join(dbPath, "telemetry.ring"))
		}
	}
}
