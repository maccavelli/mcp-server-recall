package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/mcp-server-recall/internal/config"
)

func TestEnsureInitialized_FirstRun(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	Cfg = config.New("test-init-firstrun")

	// Silence stderr
	origStderr := os.Stderr
	os.Stderr = os.NewFile(0, os.DevNull)
	defer func() { os.Stderr = origStderr }()

	if err := ensureInitialized(false); err != nil {
		t.Fatalf("ensureInitialized failed: %v", err)
	}

	expectedPath := filepath.Join(tempDir, config.Name, "recall.yaml")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Fatalf("Configuration was NOT created at: %s", expectedPath)
	}

	content, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	if !bytes.Contains(content, []byte("exportdir:")) {
		t.Errorf("expected generated template to contain exportdir")
	}
	if !bytes.Contains(content, []byte("dbpath:")) {
		t.Error("Config missing expected dbpath entry")
	}
}

func TestEnsureInitialized_ForceOverwrite(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	Cfg = config.New("test-init-overwrite-yes")

	// Silence stderr
	origStderr := os.Stderr
	os.Stderr = os.NewFile(0, os.DevNull)
	defer func() { os.Stderr = origStderr }()

	// Create initial config
	if err := ensureInitialized(false); err != nil {
		t.Fatalf("first init failed: %v", err)
	}

	configPath := filepath.Join(tempDir, config.Name, "recall.yaml")
	if err := os.WriteFile(configPath, []byte("modified: true\n"), 0600); err != nil {
		t.Fatalf("Failed to modify config: %v", err)
	}

	// In a non-TTY environment (like this test pipe), force=true will overwrite silently
	if err := ensureInitialized(true); err != nil {
		t.Fatalf("overwrite init failed: %v", err)
	}

	// Verify file was overwritten with the template
	afterContent, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config after overwrite: %v", err)
	}

	if bytes.Contains(afterContent, []byte("modified: true")) {
		t.Error("Config still contains modified content after force overwrite")
	}
	if !bytes.Contains(afterContent, []byte("apiport: 18001")) {
		t.Error("Config missing expected apiport after overwrite")
	}
}
