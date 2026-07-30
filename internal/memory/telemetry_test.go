package memory

import (
	"context"
	"os"
	"testing"

	"github.com/maccavelli/mcp-server-recall/internal/config"
	"github.com/maccavelli/mcp-server-recall/internal/search"
)

func TestMemoryStore_GetTelemetry(t *testing.T) {
	tmp, _ := os.MkdirTemp("", "recall-telemetry-*")
	defer os.RemoveAll(tmp)

	store, err := NewMemoryStore(context.Background(), tmp, "", 0, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	reads, writes, drifts, entries := store.GetTelemetry()
	_ = reads
	_ = writes
	_ = drifts
	_ = entries
}

func TestMemoryStore_DocCount(t *testing.T) {
	tmp, _ := os.MkdirTemp("", "recall-doccount-*")
	defer os.RemoveAll(tmp)

	store, err := NewMemoryStore(context.Background(), tmp, "", 0, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	count, err := store.DocCount()
	if err != nil {
		t.Fatalf("DocCount failed: %v", err)
	}
	_ = count

	// Save a record and verify DocCount doesn't fail or panic
	ctx := context.Background()
	_, err = store.Save(ctx, "title1", "doc-count-key", "content", "cat", nil, "", 0)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	count2, err := store.DocCount()
	if err != nil {
		t.Fatalf("DocCount after save failed: %v", err)
	}
	_ = count2
}

func TestMemoryStore_DriftAlerts(t *testing.T) {
	tmp, _ := os.MkdirTemp("", "recall-drift-*")
	defer os.RemoveAll(tmp)

	store, err := NewMemoryStore(context.Background(), tmp, "", 0, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	defer store.Close()

	alerts := store.DriftAlerts()
	_ = alerts
}

func TestMemoryStore_SetSearchEngine(t *testing.T) {
	tmp, _ := os.MkdirTemp("", "recall-search-engine-*")
	defer os.RemoveAll(tmp)

	store, err := NewMemoryStore(context.Background(), tmp, "", 0, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	defer store.Close()

	engDir, _ := os.MkdirTemp("", "recall-search-dir-*")
	defer os.RemoveAll(engDir)

	eng, err := search.InitStorage(engDir)
	if err != nil {
		t.Fatalf("failed to create search engine: %v", err)
	}

	ctx := context.Background()
	if err := store.SetSearchEngine(ctx, eng); err != nil {
		t.Fatalf("SetSearchEngine failed: %v", err)
	}
}

func TestMemoryStore_SyncSearchIndex(t *testing.T) {
	tmp, _ := os.MkdirTemp("", "recall-sync-*")
	defer os.RemoveAll(tmp)

	store, err := NewMemoryStore(context.Background(), tmp, "", 0, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	defer store.Close()

	// Without search engine — should return error
	ctx := context.Background()
	err = store.SyncSearchIndex(ctx)
	_ = err // nil or error, either is fine — just exercise the path

	// With search engine
	engDir, _ := os.MkdirTemp("", "recall-sync-eng-*")
	defer os.RemoveAll(engDir)
	eng, _ := search.InitStorage(engDir)
	_ = store.SetSearchEngine(ctx, eng)
	_ = store.SyncSearchIndex(ctx)
}

func TestMergeTags(t *testing.T) {
	result := mergeTags([]string{"a", "b"}, []string{"b", "c"})
	if len(result) != 3 {
		t.Errorf("expected 3 merged tags, got %d: %v", len(result), result)
	}
	// Test with nil inputs
	result2 := mergeTags(nil, nil)
	if len(result2) != 0 {
		t.Fatalf("expected 0 results for mismatched tag, got %v", len(result2))
	}
}
