// Package main provides functionality for the main subsystem.
package main

import "strings"

// RawVersion is the build-time version of the Recall MCP server. It defaults
// to "dev" so a binary built outside a tag release can never be mistaken for
// one: the previous hard-coded "v2.0.0" was the current latest tag, so every
// local build claimed to BE the newest release and self-update would have
// believed it was permanently up to date.
var RawVersion = "dev"

// RawBuildKind is "release" only for a tag build. A bool cannot be set with
// the Go linker's -X flag, so this is a string and only that exact value
// counts; anything else is a local build that update refuses to replace
// without --force.
var RawBuildKind = "local"

// Version is RawVersion without its optional v prefix.
var Version = strings.TrimPrefix(RawVersion, "v")
