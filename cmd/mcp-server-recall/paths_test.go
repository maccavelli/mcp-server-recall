//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/mcp-server-recall/internal/config"
)

func TestPaths(t *testing.T) {
	os.Setenv("XDG_CONFIG_HOME", "/tmp/config")
	defer os.Unsetenv("XDG_CONFIG_HOME")

	dir := configDirPath()
	expected := filepath.Join("/tmp/config", config.Name)
	if dir != expected {
		t.Errorf("expected %s, got %s", expected, dir)
	}

	path := configFilePath()
	expectedFile := filepath.Join(expected, "recall.yaml")
	if path != expectedFile {
		t.Errorf("expected %s, got %s", expectedFile, path)
	}

	// Test error branch
	os.Unsetenv("XDG_CONFIG_HOME")
	os.Setenv("HOME", "") // Empty HOME will cause UserConfigDir to fail on Linux

	dir = configDirPath()
	expectedFallback := filepath.Join(".", config.Name)
	if dir != expectedFallback {
		t.Errorf("expected fallback %s, got %s", expectedFallback, dir)
	}
}
