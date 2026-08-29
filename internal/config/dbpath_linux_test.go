//go:build linux

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetDBPath_IgnoresConfigDirStore(t *testing.T) {
	sandboxHome(t)

	cfgBase, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir: %v", err)
	}
	cfgDir := filepath.Join(cfgBase, Name)
	legacy := filepath.Join(cfgDir, DefaultDBName)
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatalf("mkdir leftover config-dir store: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "MANIFEST"), []byte("leftover"), 0o600); err != nil {
		t.Fatalf("write leftover MANIFEST: %v", err)
	}
	writeSandboxYAML(t, "dbpath: \"\"\n")

	work := t.TempDir()
	t.Chdir(work)

	got := New("test-ignore-legacy").GetDBPath()
	if got == legacy {
		t.Fatalf("GetDBPath() selected leftover config-dir store %q; greenfield default must ignore it", got)
	}
	want := expectedDefaultDBPath(t)
	if got != want {
		t.Errorf("GetDBPath() = %q, want data-dir default %q", got, want)
	}
}
