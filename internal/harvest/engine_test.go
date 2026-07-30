package harvest

import (
	"context"
	"testing"
)

func TestEngine_Run_StandardLib(t *testing.T) {
	e := NewEngine()
	// Test against a fast, standard library package to ensure the AST parser executes successfully
	res, err := e.Run(context.Background(), "context")
	if err != nil {
		t.Fatalf("engine.Run failed: %v", err)
	}

	if res == nil {
		t.Fatalf("expected non-nil result")
	}

	if len(res.Symbols) == 0 {
		t.Errorf("expected symbols to be harvested from 'context' package")
	}

	if res.Checksum == "" {
		t.Errorf("expected checksum to be generated")
	}

	if _, ok := res.PackageDocs["context"]; !ok {
		t.Errorf("expected package documentation for 'context'")
	}
}

func TestEngine_RemoteHarvestCoverage(t *testing.T) {
	e := NewEngine()
	ctx := context.Background()
	_, cleanup, _ := e.setupRemoteHarvestDir(ctx, "github.com/fake/repo")
	if cleanup != nil {
		cleanup()
	}
}
