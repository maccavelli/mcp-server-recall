// Package cmd provides functionality for the cmd subsystem.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/maccavelli/mcp-server-recall/internal/config"
)

// configDirPath returns the OS-idempotent config directory for the application.
//
//	Linux:   ~/.config/mcp-server-recall/
//	macOS:   ~/Library/Application Support/mcp-server-recall/
//	Windows: %AppData%\mcp-server-recall\
func configDirPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to resolve user config directory (%v), falling back to current working directory\n", err)
		configDir = "."
	}
	return filepath.Join(configDir, config.Name)
}

// configFilePath returns the full path to the recall.yaml configuration file.
func configFilePath() string {
	return filepath.Join(configDirPath(), "recall.yaml")
}
