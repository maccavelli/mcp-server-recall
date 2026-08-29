//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/mcp-server-recall/internal/config"
)

// sandboxConfigDir redirects the platform config root into a fresh temp dir
// (HOME covers macOS, XDG_CONFIG_HOME covers Linux) and returns the resulting
// os.UserConfigDir so expectations match production resolution on every OS.
func sandboxConfigDir(t *testing.T) string {
	t.Helper()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))
	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir after sandbox: %v", err)
	}
	return base
}

func TestPaths(t *testing.T) {
	base := sandboxConfigDir(t)

	dir := configDirPath()
	expected := filepath.Join(base, config.Name)
	if dir != expected {
		t.Errorf("expected %s, got %s", expected, dir)
	}

	path := configFilePath()
	expectedFile := filepath.Join(expected, "recall.yaml")
	if path != expectedFile {
		t.Errorf("expected %s, got %s", expectedFile, path)
	}

	// Test error branch — with both roots empty, UserConfigDir fails on any Unix.
	// CWD is not an acceptable fallback for user configuration.
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	dir = configDirPath()
	if dir != "" {
		t.Errorf("expected empty config dir when HOME is unset, got %s", dir)
	}
}
