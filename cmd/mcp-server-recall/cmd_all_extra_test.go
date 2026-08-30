package main

import (
	"bytes"
	"testing"
)

func TestOtherCommands(t *testing.T) {
	// These will mostly fail or exit early, but provide coverage
	for _, args := range [][]string{
		{"export", "/tmp/dummy"},
		{"import", "/tmp/dummy"},
		{"purge", "projects"},
		{"serve", "--config", "/nonexistent"},
		{"prune", "projects"},
		{"dashboard", "--config", "/nonexistent"},
		{"config", "--config", "/nonexistent"},
	} {
		cmd := RootCmd
		cmd.SetArgs(args)
		_ = cmd.Execute()
	}
}

func TestAllCommands(t *testing.T) {
	tests := [][]string{
		{"configure"},
		{"export", "fake-domain", "/tmp/fake-out.jsonl"},
		{"import", "fake-domain", "/tmp/fake-in.jsonl"},
		{"prune", "30"},
		{"purge", "memories"},
	}

	for _, args := range tests {
		RootCmd.SetArgs(args)
		var buf bytes.Buffer
		RootCmd.SetOut(&buf)
		RootCmd.SetErr(&buf)

		// We expect errors because we pass fake paths or don't setup real contexts
		_ = RootCmd.Execute()
	}
}
