package memory

import (
	"os"

	"github.com/maccavelli/mcp-server-recall/internal/config"

	"context"
	"testing"

	"github.com/maccavelli/mcp-server-recall/internal/search"
)

func TestSearch_MemoryLimitFallback(t *testing.T) {
	tmpDir := t.TempDir()

	// Set an extremely low limit (1 document)
	store, err := NewMemoryStore(context.Background(), tmpDir, "", 1, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	tmpDir2, _ := os.MkdirTemp("", "TestSearch_MemoryLimitFallback*")
	defer os.RemoveAll(tmpDir2)
	engine, _ := search.InitStorage(tmpDir2)
	if err := store.SetSearchEngine(ctx, engine); err != nil {
		t.Fatalf("failed to set search engine: %v", err)
	}

	// Save 2 documents -> should trigger fallback on next sync or rebuild
	store.Save(ctx, "", "key1", "content one", "cat", []string{"tag1"}, "", 0)
	store.Save(ctx, "", "key2", "content two", "cat", []string{"tag2"}, "", 0)

	// Force a sync
	if err := store.SyncSearchIndex(ctx); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	// Verify that store.search is now nil (fallback triggered)
	store.mu.RLock()
	sEngine := store.search
	store.mu.RUnlock()

	if sEngine != nil {
		t.Errorf("Expected search engine to be disabled (nil) after exceeding limit, but it is still active")
	}

	// Ensure search still works (falling back to fuzzy)
	resultsSeq, err := store.Search(ctx, "content", "", 10)
	if err != nil {
		t.Fatalf("search failed during fallback: %v", err)
	}
	var results []*SearchResult
	for r := range resultsSeq {
		results = append(results, r)
	}
	if len(results) == 0 {
		t.Error("Expected results from fuzzy fallback, but got none")
	}
}

func TestEncryption_StartStop(t *testing.T) {
	tmpDir := t.TempDir()
	key := "12345678901234567890123456789012" // 32 bytes

	// 1. Create encrypted store
	store, err := NewMemoryStore(context.Background(), tmpDir, key, 0, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed to create encrypted store: %v", err)
	}

	ctx := context.Background()
	if _, err := store.Save(ctx, "", "secret", "sensitive data", "top", nil, "", 0); err != nil {
		t.Fatalf("failed to save: %v", err)
	}
	store.Close()

	// 2. Re-open with WRONG key -> should fail
	wrongKey := "wrong-key-0000000000000000000000" // padding to 32
	if len(wrongKey) < 32 {
		wrongKey = wrongKey + "0000000000"
		wrongKey = wrongKey[:32]
	}

	_, err = NewMemoryStore(context.Background(), tmpDir, wrongKey, 0, config.New("test").BatchSettings())
	if err == nil {
		t.Errorf("Expected failure when opening encrypted DB with wrong key, but it succeeded")
	}

	// 3. Re-open with CORRECT key -> should succeed
	store2, err := NewMemoryStore(context.Background(), tmpDir, key, 0, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed to re-open with correct key: %v", err)
	}
	defer store2.Close()

	rec, err := store2.Get(ctx, "secret")
	if err != nil {
		t.Fatalf("failed to get data after re-open: %v", err)
	}
	if rec.Content != "sensitive data" {
		t.Errorf("Data mismatch after re-open: got %q, want %q", rec.Content, "sensitive data")
	}
}
