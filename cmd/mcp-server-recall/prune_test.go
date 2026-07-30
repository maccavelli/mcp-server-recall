package main

import (
	"testing"
)

func TestPruneCmd(t *testing.T) {
	err := pruneCmd.RunE(pruneCmd, []string{"test_namespace", "-1"})
	if err == nil {
		t.Errorf("Expected error for invalid days")
	}

	err = pruneCmd.RunE(pruneCmd, []string{"test_namespace", "abc"})
	if err == nil {
		t.Errorf("Expected error for non-integer days")
	}
}

func TestRunPruneViaMCP(t *testing.T) {
	// Should fail quickly and print error
	runPruneViaMCP("sessions", 10)
}
