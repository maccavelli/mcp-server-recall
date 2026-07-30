// Package main provides functionality for the main subsystem.
package main

import "strings"

// Version is the current version of the Recall MCP server.
var RawVersion = "v4.3.4"
var Version = strings.TrimPrefix(RawVersion, "v")
