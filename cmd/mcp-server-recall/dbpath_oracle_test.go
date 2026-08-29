package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/maccavelli/mcp-server-recall/internal/config"
)

// expectedDefaultDBPath is the MADR 0002 platform table, computed independently of
// production resolvers so a wrong helper cannot hide a wrong GetDBPath.
func expectedDefaultDBPath(t *testing.T) string {
	t.Helper()
	switch runtime.GOOS {
	case "windows":
		base, err := os.UserCacheDir()
		if err != nil {
			t.Fatalf("UserCacheDir: %v", err)
		}
		return filepath.Join(base, config.Name, config.DefaultDBName)
	case "darwin":
		base, err := os.UserConfigDir()
		if err != nil {
			t.Fatalf("UserConfigDir: %v", err)
		}
		return filepath.Join(base, config.Name, config.DefaultDBName)
	default:
		data := os.Getenv("XDG_DATA_HOME")
		if data == "" || !filepath.IsAbs(data) {
			home, err := os.UserHomeDir()
			if err != nil {
				t.Fatalf("UserHomeDir: %v", err)
			}
			data = filepath.Join(home, ".local", "share")
		}
		return filepath.Join(data, config.Name, config.DefaultDBName)
	}
}
