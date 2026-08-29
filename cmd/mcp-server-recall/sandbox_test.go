package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// sandboxConfigDir redirects the platform config (and data) roots into a fresh
// temp dir and returns os.UserConfigDir so expectations match production
// resolution on every OS. Windows ignores HOME/XDG; AppData and LocalAppData
// must be set or tests write into the real user profile.
func sandboxConfigDir(t *testing.T) string {
	t.Helper()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tempHome, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tempHome, ".cache"))
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", tempHome)
		t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))
		t.Setenv("LocalAppData", filepath.Join(tempHome, "AppData", "Local"))
	}
	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir after sandbox: %v", err)
	}
	if !filepath.IsAbs(base) {
		t.Fatalf("UserConfigDir after sandbox is not absolute: %q", base)
	}
	return base
}
