package main

import (
	"context"
	"testing"
)

func TestStartStreamableHTTPAPI(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errChan := make(chan error, 1)

	// Assuming it can handle a nil server or we can create an empty one
	srv := startStreamableHTTPAPI(ctx, nil, errChan, 0) // Port 0 for random available port
	if srv == nil {
		t.Fatal("expected server to be created")
	}

	// Close the server
	_ = srv.Close()
}
