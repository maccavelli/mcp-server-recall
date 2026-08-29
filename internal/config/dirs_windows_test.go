//go:build windows

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDataDir_UsesLocalAppData(t *testing.T) {
	sandboxHome(t)

	data, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir: %v", err)
	}
	local, err := os.UserCacheDir()
	if err != nil {
		t.Fatalf("UserCacheDir: %v", err)
	}
	want := filepath.Join(local, Name)
	if data != want {
		t.Errorf("DataDir() = %q, want under LocalAppData %q", data, want)
	}

	roaming, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir: %v", err)
	}
	if roaming != local && data == filepath.Join(roaming, Name) {
		t.Errorf("DataDir() used Roaming AppData %q", data)
	}
}
