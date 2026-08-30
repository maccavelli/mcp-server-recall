// Package main provides functionality for the main subsystem.
package main

import "strings"

// RawVersion is the build-time version of the Recall MCP server.
var RawVersion = "v2.0.0"

// Version is RawVersion without its optional v prefix.
var Version = strings.TrimPrefix(RawVersion, "v")
