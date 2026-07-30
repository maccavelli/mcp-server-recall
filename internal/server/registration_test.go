package server

import (
	"testing"
)

func TestReloadTools(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()

	// ReloadTools doesn't return an error, just checks nil map etc
	// We call it to ensure it does not panic
	srv.ReloadTools()
}
