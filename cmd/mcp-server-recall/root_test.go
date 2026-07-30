package main

import (
	"testing"
)

func TestInitConfig(t *testing.T) {
	// Call initConfig to ensure it populates Cfg
	initConfig()
	if Cfg == nil {
		t.Fatal("expected Cfg to be initialized")
	}
	if Cfg.Name() == "" {
		t.Error("expected Cfg Name to be set")
	}
}

func TestExecute(t *testing.T) {
	// Set args to --help so it doesn't block or error out
	RootCmd.SetArgs([]string{"--help"})
	Execute()
}
