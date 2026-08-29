// Package cmd provides functionality for the cmd subsystem.
package main

import (
	"path/filepath"

	"github.com/maccavelli/mcp-server-recall/internal/config"
)

// configDirPath returns the OS-idempotent config directory for the application.
//
//	Linux:   ~/.config/mcp-server-recall/
//	macOS:   ~/Library/Application Support/mcp-server-recall/
//	Windows: %AppData%\mcp-server-recall\
func configDirPath() string {
	dir, err := config.ConfigDir()
	if err != nil {
		return ""
	}
	return dir
}

// configFilePath returns the full path to the recall.yaml configuration file.
func configFilePath() string {
	return filepath.Join(configDirPath(), "recall.yaml")
}
