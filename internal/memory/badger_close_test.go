package memory

import (
	"context"
	"runtime"
	"testing"

	"github.com/maccavelli/mcp-server-recall/internal/config"
)

func TestMemoryStore_CloseIsIdempotent(t *testing.T) {
	s, err := NewMemoryStore(context.Background(), t.TempDir(), "", 0, config.BatchConfig{})
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestMemoryStore_CloseThenGCReopen is the materializeStore pattern that failed
// CI: explicit Close followed by AddCleanup closing the same MANIFEST fd after
// reuse (TestConfigure_KeyRoundTrips/mixed: "MANIFEST: bad file descriptor").
func TestMemoryStore_CloseThenGCReopen(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	key := "8e71e69965ade5e8fd42c399212ed45e324bfe9e41ca2d32266a9d7ebd2dacc0"

	s, err := NewMemoryStore(ctx, dir, key, 0, config.BatchConfig{})
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s = nil
	runtime.GC()
	runtime.GC()

	s2, err := NewMemoryStore(ctx, dir, key, 0, config.BatchConfig{})
	if err != nil {
		t.Fatalf("reopen after Close+GC: %v", err)
	}
	if err := s2.Close(); err != nil {
		t.Fatalf("reopen Close: %v", err)
	}
}
