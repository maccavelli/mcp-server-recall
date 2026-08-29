// Package harvest provides functionality for the harvest subsystem.
package harvest

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var (
	goBinOnce sync.Once
	goBin     string
)

// resolveGoBin returns the absolute path to the Go toolchain binary.
// Resolution priority:
//  1. MCP_GO_BIN_PATH env var (explicit operator override — checked first)
//  2. $GOROOT/bin/go (standard Go env var)
//  3. Common SDK install locations (~/sdk/go*, ~/.local/go, /usr/local/go, etc.)
//  4. exec.LookPath("go") — inherited PATH as last resort
//
// The result is cached after the first call via sync.Once.
func resolveGoBin() string {
	goBinOnce.Do(func() {
		// 1. Explicit operator override
		if v := os.Getenv("MCP_GO_BIN_PATH"); v != "" {
			slog.Info("go toolchain resolved via MCP_GO_BIN_PATH", "path", v)
			goBin = v
			return
		}

		name := goBinaryName()

		// 2. Derive from GOROOT if set
		if root := os.Getenv("GOROOT"); root != "" {
			candidate := filepath.Join(root, "bin", name)
			if isExecutable(candidate) {
				slog.Info("go toolchain resolved via GOROOT", "path", candidate)
				goBin = candidate
				return
			}
		}

		// 3. Known install locations, ordered by preference
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			slog.Warn("failed to resolve user home for go toolchain discovery", "error", homeErr)
			return
		}
		candidates := []string{
			filepath.Join(home, "sdk", "go1.26.5", "bin", name),
			filepath.Join(home, "sdk", "go1.26.1", "bin", name),
			filepath.Join(home, "sdk", "go1.25.0", "bin", name),
			filepath.Join(home, ".local", "go", "bin", name),
			filepath.Join(home, "go", "bin", name),
		}
		if !runningOnWindows() {
			candidates = append(candidates, "/usr/local/go/bin/go", "/usr/lib/go/bin/go")
		}

		// Glob for any sdk/go* directories so version upgrades are picked up automatically
		if matches, err := filepath.Glob(filepath.Join(home, "sdk", "go*", "bin", name)); err == nil {
			candidates = append(matches, candidates...)
		}

		for _, c := range candidates {
			if isExecutable(c) {
				slog.Info("go toolchain resolved via filesystem scan", "path", c)
				goBin = c
				return
			}
		}

		// 4. Last resort: PATH lookup
		if path, err := exec.LookPath("go"); err == nil {
			slog.Info("go toolchain resolved via PATH", "path", path)
			goBin = path
			return
		}

		slog.Warn("go toolchain not found; set MCP_GO_BIN_PATH to the absolute path of your go binary")
		goBin = ""
	})
	return goBin
}

func runningOnWindows() bool {
	return runtime.GOOS == "windows"
}

func goBinaryName() string {
	if runningOnWindows() {
		return "go.exe"
	}
	return "go"
}

// isExecutable reports whether path exists and is a regular executable file.
// Windows has no Unix execute bit; PATHEXT decides whether a file is runnable.
func isExecutable(path string) bool {
	info, err := os.Stat(path) //nolint:gosec // candidate go binary paths are from a fixed allowlist
	if err != nil {
		return false
	}
	if !info.Mode().IsRegular() {
		return false
	}
	if !runningOnWindows() {
		return info.Mode()&0o111 != 0
	}
	ext := filepath.Ext(path)
	if ext == "" {
		return false
	}
	pathext := os.Getenv("PATHEXT")
	if pathext == "" {
		pathext = ".COM;.EXE;.BAT;.CMD"
	}
	for _, pe := range strings.Split(pathext, ";") {
		if strings.EqualFold(pe, ext) {
			return true
		}
	}
	return false
}

// goEnv returns os.Environ() with the resolved Go binary's parent directory
// prepended to PATH. It also mutates the current process PATH via os.Setenv
// so that go/packages' internal exec.LookPath calls find the right binary.
func goEnv() []string {
	bin := resolveGoBin()
	if bin == "" {
		return os.Environ()
	}
	dir := filepath.Dir(bin)

	// Mutate the current process PATH so go/packages' own LookPath succeeds
	existing := os.Getenv("PATH")
	if existing == "" {
		if setErr := os.Setenv("PATH", dir); setErr != nil {
			slog.Debug("failed to prepend go bin to PATH", "error", setErr)
		}
	} else {
		pathList := filepath.SplitList(existing)
		if len(pathList) == 0 || pathList[0] != dir {
			if setErr := os.Setenv("PATH", dir+string(filepath.ListSeparator)+existing); setErr != nil {
				slog.Debug("failed to prepend go bin to PATH", "error", setErr)
			}
		}
	}

	return os.Environ()
}
