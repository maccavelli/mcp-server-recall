//go:build !windows

// Package util provides functionality for the util subsystem.
package util

import (
	"os"
	"syscall"
)

func mirrorOwnership(filename string, stat os.FileInfo) {
	if sys, ok := stat.Sys().(*syscall.Stat_t); ok {
		if err := os.Chown(filename, int(sys.Uid), int(sys.Gid)); err != nil {
			return
		}
	}
}
