//go:build windows

// Package util provides functionality for the util subsystem.
package util

import "os"

func mirrorOwnership(filename string, stat os.FileInfo) {
	// Not implemented for Windows
}
