package harvest

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestGoBin(t *testing.T) {
	// Cleanup global state
	defer func() {
		goBinOnce = sync.Once{}
		goBin = ""
	}()

	// Test goEnv
	_ = goEnv()

	tmpDir := t.TempDir()

	goBinName := goBinaryName()
	dummyGo := filepath.Join(tmpDir, goBinName)
	if err := os.WriteFile(dummyGo, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("failed to create dummy go: %v", err)
	}

	// 1. Test MCP_GO_BIN_PATH
	t.Run("MCP_GO_BIN_PATH", func(t *testing.T) {
		goBinOnce = sync.Once{}
		old := os.Getenv("MCP_GO_BIN_PATH")
		defer os.Setenv("MCP_GO_BIN_PATH", old)

		os.Setenv("MCP_GO_BIN_PATH", "/fake/path/go")
		if got := resolveGoBin(); got != "/fake/path/go" {
			t.Errorf("expected /fake/path/go, got %s", got)
		}
	})

	// 2. Test GOROOT
	t.Run("GOROOT", func(t *testing.T) {
		goBinOnce = sync.Once{}
		oldMCP := os.Getenv("MCP_GO_BIN_PATH")
		defer os.Setenv("MCP_GO_BIN_PATH", oldMCP)
		os.Unsetenv("MCP_GO_BIN_PATH")

		oldRoot := os.Getenv("GOROOT")
		defer os.Setenv("GOROOT", oldRoot)

		// Setup fake GOROOT
		fakeRoot := filepath.Join(tmpDir, "fakegoroot")
		binDir := filepath.Join(fakeRoot, "bin")
		os.MkdirAll(binDir, 0755)
		fakeGo := filepath.Join(binDir, goBinaryName())
		os.WriteFile(fakeGo, []byte("#!/bin/sh\n"), 0755)

		os.Setenv("GOROOT", fakeRoot)
		if got := resolveGoBin(); got != fakeGo {
			t.Errorf("expected %s, got %s", fakeGo, got)
		}
	})

	// 3. Test PATH
	t.Run("PATH", func(t *testing.T) {
		goBinOnce = sync.Once{}
		oldMCP := os.Getenv("MCP_GO_BIN_PATH")
		defer os.Setenv("MCP_GO_BIN_PATH", oldMCP)
		os.Unsetenv("MCP_GO_BIN_PATH")

		oldRoot := os.Getenv("GOROOT")
		defer os.Setenv("GOROOT", oldRoot)
		os.Unsetenv("GOROOT")

		// To avoid hitting sdk paths, we can mock home or just let it fall through
		oldHome := os.Getenv("HOME")
		defer os.Setenv("HOME", oldHome)
		os.Setenv("HOME", "/nonexistent/fake/home")

		oldPath := os.Getenv("PATH")
		defer os.Setenv("PATH", oldPath)
		os.Setenv("PATH", tmpDir)

		_ = resolveGoBin()
	})

	// Test isExecutable
	if !isExecutable(dummyGo) {
		t.Errorf("expected dummy go to be executable")
	}

	nonExec := filepath.Join(tmpDir, "nonexec")
	_ = os.WriteFile(nonExec, []byte("test"), 0644)
	if isExecutable(nonExec) {
		t.Errorf("expected nonexec to not be executable")
	}

	if isExecutable(filepath.Join(tmpDir, "does-not-exist")) {
		t.Errorf("expected non-existent file to not be executable")
	}
}

func TestGoEnv(t *testing.T) {
	goBinOnce = sync.Once{}

	override := filepath.Join(t.TempDir(), goBinaryName())
	t.Setenv("MCP_GO_BIN_PATH", override)
	env := goEnv()
	if env == nil {
		t.Error("expected non-nil environment")
	}

	// Now check if it mutated PATH
	path := os.Getenv("PATH")
	wantDir := filepath.Dir(override)
	if !strings.Contains(path, wantDir) {
		t.Errorf("expected PATH to contain %s, got %s", wantDir, path)
	}

	// Test without MCP_GO_BIN_PATH
	goBinOnce = sync.Once{}
	t.Setenv("MCP_GO_BIN_PATH", "")
	env2 := goEnv()
	if env2 == nil {
		t.Error("expected non-nil environment")
	}
}
