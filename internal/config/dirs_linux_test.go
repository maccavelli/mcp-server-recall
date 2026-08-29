//go:build linux

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDataDir_NotConfigDir(t *testing.T) {
	sandboxHome(t)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	data, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir: %v", err)
	}
	cfg, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	if data == cfg {
		t.Fatalf("DataDir and ConfigDir both %q; Linux data must not live under config", data)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	want := filepath.Join(home, ".local", "share", Name)
	if data != want {
		t.Errorf("DataDir() = %q, want %q", data, want)
	}
}

func TestDataDir_XDGDataHomeWins(t *testing.T) {
	sandboxHome(t)
	xdg := filepath.Join(t.TempDir(), "custom-data")
	t.Setenv("XDG_DATA_HOME", xdg)

	data, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir: %v", err)
	}
	want := filepath.Join(xdg, Name)
	if data != want {
		t.Errorf("DataDir() = %q, want %q", data, want)
	}
}

func TestDataDir_RelativeXDGIgnored(t *testing.T) {
	sandboxHome(t)
	t.Setenv("XDG_DATA_HOME", "relative-data")

	data, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	want := filepath.Join(home, ".local", "share", Name)
	if data != want {
		t.Errorf("DataDir() = %q, want %q (relative XDG_DATA_HOME must be ignored)", data, want)
	}
}
