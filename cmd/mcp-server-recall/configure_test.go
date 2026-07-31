package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/mcp-server-recall/internal/config"
)

func TestConfigureCommand_Sandboxed(t *testing.T) {
	// 1. Enforce strict sandboxing of the platform config root
	base := sandboxConfigDir(t)

	// Inject isolated config container successfully to bypass existingKey checks safely
	Cfg = config.New("test-sandboxed")

	// 2. Silence test output noise strictly
	originalStderr := os.Stderr
	os.Stderr = os.NewFile(0, os.DevNull)
	defer func() { os.Stderr = originalStderr }()

	// 3. Pre-create the config file via init (required by the configure guard)
	if err := ensureInitialized(true); err != nil {
		t.Fatalf("ensureInitialized pre-setup failed: %v", err)
	}

	if err := ensureInitialized(false); err != nil {
		t.Fatalf("ensureInitialized with false should just return nil: %v", err)
	}

	// 4. In a non-TTY environment, configureCmd will automatically skip interactive prompts and default to no encryption

	// 5. Manually execute the cobra command purely inside memory
	err := configureCmd.RunE(configureCmd, []string{})
	if err != nil {
		t.Fatalf("configureCmd failed natively: %v", err)
	}

	// 6. Assert the artifact was written inside the sandboxed config root
	expectedPath := filepath.Join(base, config.Name, "recall.yaml")

	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Fatalf("Configuration artifact was NOT written to the sandboxed path: %s", expectedPath)
	}

	// 7. Assert standard integrity
	content, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("Failed to read sandboxed config structurally: %v", err)
	}

	if !bytes.Contains(content, []byte("encryptionkey: \"\"")) && !bytes.Contains(content, []byte("encryptionkey: \n")) && !bytes.Contains(content, []byte("encryptionkey:  ")) && !bytes.Contains(content, []byte("encryptionkey:\n")) {
		t.Errorf("Sanbox configuration artifact did not contain an explicitly blank encryptionkey entry")
	}
}
