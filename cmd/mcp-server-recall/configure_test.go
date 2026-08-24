package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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

// grepLine returns the first line of content containing needle, for legible failures.
func grepLine(content []byte, needle string) string {
	for _, line := range strings.Split(string(content), "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return "(no line matching " + needle + ")"
}

// TestConfigure_KeyRoundTrips asserts a key written by the wizard is readable by the
// config loader. See docs/0001-MADR-encryptionkey-yaml-tag-round-trip.md: the wizard
// previously re-emitted the template's !!null tag alongside the new string value,
// producing a file its own typed decode rejected.
func TestConfigure_KeyRoundTrips(t *testing.T) {
	cases := []struct{ name, key string }{
		{"mixed", "8e71e69965ade5e8fd42c399212ed45e324bfe9e41ca2d32266a9d7ebd2dacc0"},
		{"digits_only", strings.Repeat("1", 64)}, // would resolve as a number if emitted untagged
		{"all_f", strings.Repeat("f", 64)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := sandboxConfigDir(t)
			Cfg = config.New("test-roundtrip")

			t.Setenv("RECALL_ENCRYPTION_KEY", tc.key) // env branch, configure.go:63

			if err := ensureInitialized(true); err != nil {
				t.Fatalf("ensureInitialized: %v", err)
			}
			if err := configureCmd.RunE(configureCmd, []string{}); err != nil {
				t.Fatalf("configure: %v", err)
			}

			cfgPath := filepath.Join(base, config.Name, "recall.yaml")
			content, err := os.ReadFile(cfgPath)
			if err != nil {
				t.Fatalf("read written config: %v", err)
			}
			if bytes.Contains(content, []byte("encryptionkey: !!null")) {
				t.Errorf("key written with a !!null tag: %s", grepLine(content, "encryptionkey"))
			}

			// The assertion that matters: does the key survive write -> read?
			reloaded := config.New("test-roundtrip")
			if got := reloaded.EncryptionKey(); got != tc.key {
				t.Errorf("key did not survive round trip:\n  got  %q\n  want %q\n  file: %s",
					got, tc.key, grepLine(content, "encryptionkey"))
			}
		})
	}
}

// TestConfigure_NonInteractiveRefusesToClobber asserts the wizard will not silently strip an
// existing encryption key when stdin is not a terminal.
func TestConfigure_NonInteractiveRefusesToClobber(t *testing.T) {
	base := sandboxConfigDir(t)
	Cfg = config.New("test-clobber")
	key := strings.Repeat("a", 64)

	t.Setenv("RECALL_ENCRYPTION_KEY", key)
	if err := ensureInitialized(true); err != nil {
		t.Fatalf("ensureInitialized: %v", err)
	}
	if err := configureCmd.RunE(configureCmd, []string{}); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	cfgPath := filepath.Join(base, config.Name, "recall.yaml")
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read seeded config: %v", err)
	}

	// configure.go:62 tests envKey != "", so an empty value takes the non-interactive branch.
	t.Setenv("RECALL_ENCRYPTION_KEY", "")

	if err := configureCmd.RunE(configureCmd, []string{}); err == nil {
		t.Fatal("expected refusal when clobbering an existing key non-interactively")
	}

	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config after refusal: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("config modified despite refusal:\n  before: %s\n  after:  %s",
			grepLine(before, "encryptionkey"), grepLine(after, "encryptionkey"))
	}
}

// TestConfigure_NonInteractiveAllowsExplicitOptOut asserts --allow-unencrypted is the escape
// hatch for callers that genuinely want an unencrypted store non-interactively.
func TestConfigure_NonInteractiveAllowsExplicitOptOut(t *testing.T) {
	base := sandboxConfigDir(t)
	Cfg = config.New("test-optout")

	t.Setenv("RECALL_ENCRYPTION_KEY", strings.Repeat("b", 64))
	if err := ensureInitialized(true); err != nil {
		t.Fatalf("ensureInitialized: %v", err)
	}
	if err := configureCmd.RunE(configureCmd, []string{}); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	// The flag lives on a package-level cobra command shared across tests; restore it.
	allowUnencrypted = true
	t.Cleanup(func() { allowUnencrypted = false })
	t.Setenv("RECALL_ENCRYPTION_KEY", "")

	if err := configureCmd.RunE(configureCmd, []string{}); err != nil {
		t.Fatalf("configure with --allow-unencrypted should succeed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(base, config.Name, "recall.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if got := grepLine(content, "encryptionkey"); !strings.Contains(got, `encryptionkey: ""`) {
		t.Errorf("expected a blank key after explicit opt-out, got: %s", got)
	}
}
