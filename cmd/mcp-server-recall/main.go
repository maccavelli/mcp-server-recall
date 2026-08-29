// Package main provides functionality for the main subsystem.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/maccavelli/mcp-server-recall/internal/config"
)

func main() {
	// Defense-in-depth: Unmanaged Standalone Fallbacks
	if _, exists := os.LookupEnv("GOMEMLIMIT"); !exists {
		os.Setenv("GOMEMLIMIT", "1024MiB")
	}
	if _, exists := os.LookupEnv("GOMAXPROCS"); !exists {
		os.Setenv("GOMAXPROCS", "2")
	}

	// Crash log uses os.UserCacheDir for user-scoped, symlink-safe paths.
	// Linux: ~/.cache/mcp-server-recall/crash.log
	// macOS: ~/Library/Caches/mcp-server-recall/crash.log
	// Windows: %LocalAppData%\mcp-server-recall\crash.log
	crashDir := filepath.Join(os.TempDir(), config.Name) // fallback
	if cacheDir, err := config.CacheDir(); err == nil {
		crashDir = cacheDir
	}
	_ = os.MkdirAll(crashDir, 0o700)
	crashPath := filepath.Join(crashDir, "crash.log")

	f, err := os.OpenFile(crashPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err == nil {
		fmt.Fprintf(f, "MAIN STARTING args: %v\n", os.Args)
		f.Close()
	}
	defer func() {
		if f2, err := os.OpenFile(crashPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
			fmt.Fprintf(f2, "MAIN EXITED\n")
			f2.Close()
		}
	}()
	Execute()
}
