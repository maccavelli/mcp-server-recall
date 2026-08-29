//go:build darwin

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDataDir_IgnoresXDGOnDarwin(t *testing.T) {
	sandboxHome(t)
	home := os.Getenv("HOME")
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg-data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg-config"))

	dir, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir: %v", err)
	}
	if !strings.Contains(dir, filepath.Join("Library", "Application Support")) {
		t.Errorf("DataDir() = %q, want Application Support on Darwin", dir)
	}
	if strings.Contains(dir, "xdg-data") || strings.Contains(dir, "xdg-config") {
		t.Errorf("DataDir() honoured XDG on Darwin: %q", dir)
	}

	cfg, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	if dir != cfg {
		t.Errorf("DataDir() = %q, ConfigDir() = %q; Darwin should use the same tree", dir, cfg)
	}
}
