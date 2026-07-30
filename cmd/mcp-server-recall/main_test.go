package main

import (
	"testing"
)

func TestMainFunc(t *testing.T) {
	// RootCmd args have already been set to --help in root_test.go,
	// but let's be safe and set them again
	RootCmd.SetArgs([]string{"--help"})
	main()
}
