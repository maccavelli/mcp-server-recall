package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func sandboxHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
		t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))
		t.Setenv("LocalAppData", filepath.Join(home, "AppData", "Local"))
	}
}

// expectedDefaultDBPath is the MADR 0002 platform table, computed independently of
// production resolvers so a wrong helper cannot hide a wrong GetDBPath.
func expectedDefaultDBPath(t *testing.T) string {
	t.Helper()
	switch runtime.GOOS {
	case "windows":
		base, err := os.UserCacheDir()
		if err != nil {
			t.Fatalf("UserCacheDir: %v", err)
		}
		return filepath.Join(base, Name, DefaultDBName)
	case "darwin":
		base, err := os.UserConfigDir()
		if err != nil {
			t.Fatalf("UserConfigDir: %v", err)
		}
		return filepath.Join(base, Name, DefaultDBName)
	default:
		data := os.Getenv("XDG_DATA_HOME")
		if data == "" || !filepath.IsAbs(data) {
			home, err := os.UserHomeDir()
			if err != nil {
				t.Fatalf("UserHomeDir: %v", err)
			}
			data = filepath.Join(home, ".local", "share")
		}
		return filepath.Join(data, Name, DefaultDBName)
	}
}

func writeSandboxYAML(t *testing.T, contents string) {
	t.Helper()
	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir: %v", err)
	}
	dir := filepath.Join(base, Name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	path := filepath.Join(dir, "recall.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write recall.yaml: %v", err)
	}
}

func TestGetDBPath_EmptyYAMLIsNotCWD(t *testing.T) {
	sandboxHome(t)
	writeSandboxYAML(t, "dbpath: \"\"\n")

	work := t.TempDir()
	t.Chdir(work)

	got := New("test-empty-dbpath").GetDBPath()
	if got == work {
		t.Fatalf("GetDBPath() resolved empty dbpath to CWD %q", got)
	}
	want := expectedDefaultDBPath(t)
	if got != want {
		t.Errorf("GetDBPath() = %q, want platform data-dir default %q", got, want)
	}
}

func TestGetDBPath_AbsentFileUsesDefault(t *testing.T) {
	sandboxHome(t)
	work := t.TempDir()
	t.Chdir(work)

	got := New("test-absent-yaml").GetDBPath()
	if got == work {
		t.Fatalf("GetDBPath() with no config file resolved to CWD %q", got)
	}
	want := expectedDefaultDBPath(t)
	if got != want {
		t.Errorf("GetDBPath() = %q, want platform data-dir default %q", got, want)
	}
}

func TestGetDBPath_AbsoluteWins(t *testing.T) {
	sandboxHome(t)
	abs := filepath.Join(t.TempDir(), "explicit-store")
	writeSandboxYAML(t, "dbpath: "+abs+"\n")

	work := t.TempDir()
	t.Chdir(work)

	got := New("test-absolute-dbpath").GetDBPath()
	if got != abs {
		t.Errorf("GetDBPath() = %q, want explicit absolute %q", got, abs)
	}
}
