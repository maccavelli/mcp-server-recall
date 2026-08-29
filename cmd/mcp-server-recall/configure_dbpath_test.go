package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/mcp-server-recall/internal/config"
)

func TestConfigure_MaterializesBadgerManifest(t *testing.T) {
	base := sandboxConfigDir(t)
	Cfg = config.New("test-materialize")

	origStderr := os.Stderr
	os.Stderr = os.NewFile(0, os.DevNull)
	defer func() { os.Stderr = origStderr }()

	work := t.TempDir()
	t.Chdir(work)

	key := strings.Repeat("a", 64)
	t.Setenv("RECALL_ENCRYPTION_KEY", key)

	if err := ensureInitialized(true); err != nil {
		t.Fatalf("ensureInitialized: %v", err)
	}
	if err := configureCmd.RunE(configureCmd, []string{}); err != nil {
		t.Fatalf("configure: %v", err)
	}

	// Reload so GetDBPath reflects the written file, not the process CWD.
	Cfg = config.New("test-materialize-reload")
	dbPath := Cfg.GetDBPath()
	if dbPath == work {
		t.Fatalf("GetDBPath() is CWD %q after configure", dbPath)
	}
	want := expectedDefaultDBPath(t)
	if dbPath != want {
		t.Errorf("GetDBPath() = %q, want %q", dbPath, want)
	}

	if _, err := os.Stat(filepath.Join(dbPath, "MANIFEST")); err != nil {
		t.Errorf("expected Badger MANIFEST under %s: %v", dbPath, err)
	}
	if _, err := os.Stat(filepath.Join(dbPath, "KEYREGISTRY")); err != nil {
		t.Errorf("expected Badger KEYREGISTRY under %s: %v", dbPath, err)
	}

	cfgPath := filepath.Join(base, config.Name, "recall.yaml")
	if _, err := os.Stat(cfgPath); err != nil {
		t.Errorf("expected config at %s: %v", cfgPath, err)
	}
}

func TestConfigure_RecreatesMissingDataDir(t *testing.T) {
	_ = sandboxConfigDir(t)
	Cfg = config.New("test-recreate-datadir")

	origStderr := os.Stderr
	os.Stderr = os.NewFile(0, os.DevNull)
	defer func() { os.Stderr = origStderr }()

	if err := ensureInitialized(false); err != nil {
		t.Fatalf("ensureInitialized: %v", err)
	}

	dbPath := expectedDefaultDBPath(t)
	if err := os.RemoveAll(dbPath); err != nil {
		t.Fatalf("remove data dir: %v", err)
	}

	if err := configureCmd.RunE(configureCmd, []string{}); err != nil {
		t.Fatalf("configure after missing data dir: %v", err)
	}

	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("data dir was not recreated at %s: %v", dbPath, err)
	}
	if !info.IsDir() {
		t.Errorf("data path %s is not a directory", dbPath)
	}
}
