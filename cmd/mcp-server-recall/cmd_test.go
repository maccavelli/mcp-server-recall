package main

import "testing"

func TestCLI_CommandVars(t *testing.T) {
	if RootCmd == nil {
		t.Error("expected valid Root command")
	}

	if serveCmd == nil {
		t.Error("expected valid Serve command")
	}

	if harvestCmd == nil {
		t.Error("expected valid Harvest command")
	}

	if configureCmd == nil {
		t.Error("expected valid Configure command")
	}

	if purgeCmd == nil {
		t.Error("expected valid Purge command")
	}
}
