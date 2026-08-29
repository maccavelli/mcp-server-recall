package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/mcp-server-recall/internal/config"
)

func TestPurge_EmptyDBPathDoesNotDeleteCWD(t *testing.T) {
	_ = sandboxConfigDir(t)

	cfgBase, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir: %v", err)
	}
	cfgDir := filepath.Join(cfgBase, config.Name)
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "recall.yaml"), []byte("dbpath: \"\"\n"), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	work := t.TempDir()
	canary := filepath.Join(work, "canary.txt")
	if err := os.WriteFile(canary, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write canary: %v", err)
	}
	t.Chdir(work)

	Cfg = config.New("test-purge-cwd")
	forceFlag = true
	t.Cleanup(func() { forceFlag = false })

	err = purgeCmd.RunE(purgeCmd, []string{})
	if _, statErr := os.Stat(canary); statErr != nil {
		t.Fatalf("purge deleted CWD canary at %s (GetDBPath=%q): %v", canary, Cfg.GetDBPath(), statErr)
	}
	if err == nil {
		t.Fatal("expected purge to refuse empty dbpath resolving toward CWD, got nil error")
	}
}
