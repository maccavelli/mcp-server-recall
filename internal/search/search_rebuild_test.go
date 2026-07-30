package search

import (
	"context"
	"testing"
)

func TestRebuild(t *testing.T) {
	e, err := InitStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	docs := map[string]*Document{
		"1": {Title: "hello", Content: "world"},
	}

	err = e.Rebuild(ctx, docs)
	if err != nil {
		t.Fatalf("Rebuild with docs failed: %v", err)
	}

	err = e.Rebuild(ctx, map[string]*Document{})
	if err != nil {
		t.Fatalf("Rebuild with empty docs failed: %v", err)
	}

	bestEffortRemoveAll("/invalid/path/that/does/not/exist")
}
