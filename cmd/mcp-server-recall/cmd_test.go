package main

import "testing"

func TestCLI_CommandVars(t *testing.T) {
	if RootCmd == nil {
		t.Error("expected valid Root command")
	}

	if serveCmd == nil {
		t.Error("expected valid Serve command")
	}

	if configureCmd == nil {
		t.Error("expected valid Configure command")
	}

	if purgeCmd == nil {
		t.Error("expected valid Purge command")
	}
}

// TestHarvestCommandIsGone pins the removal from 0005-MADR. Absence is not
// otherwise provable here: the coverage lists call cmd.Execute() and discard
// the unknown-command error.
func TestHarvestCommandIsGone(t *testing.T) {
	for _, name := range []string{"harvest"} {
		found, _, err := RootCmd.Find([]string{name})
		if err == nil && found != nil && found.Name() == name {
			t.Errorf("command %q still registered on RootCmd", name)
		}
	}
}
