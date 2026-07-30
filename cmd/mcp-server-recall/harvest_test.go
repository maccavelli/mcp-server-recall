package main

import (
	"testing"
)

func TestHarvestCmd(t *testing.T) {
	// Not much to test without starting the client, but let's test that the struct exists
	if harvestStandardsCmd.Use != "standards [package-path]" {
		t.Errorf("Unexpected use string")
	}
	if harvestProjectsCmd.Use != "projects [package-path]" {
		t.Errorf("Unexpected use string")
	}
}

func TestRunHarvestViaMCP(t *testing.T) {
	// Should fail quickly and print error
	runHarvestViaMCP("nonexistent/path", "standards")
}
