package memory

import (
	"github.com/maccavelli/mcp-server-recall/internal/config"

	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryStore_ExportJSONL(t *testing.T) {
	// 1. Setup store
	tmpDBDir, err := os.MkdirTemp("", "recall-portability-db-*")
	if err != nil {
		t.Fatalf("failed to create temp db dir: %v", err)
	}
	defer os.RemoveAll(tmpDBDir)

	store, err := NewMemoryStore(context.Background(), tmpDBDir, "", 0, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed to create memory store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// 2. Seed Data
	store.Save(ctx, "", "export-1", "value 1", "test", []string{"tag1"}, "", 0)
	store.Save(ctx, "", "export-2", "value 2", "test", []string{"tag2"}, "", 0)

	// 3. Setup Export Target
	tmpExportDir, err := os.MkdirTemp("", "recall-portability-export-*")
	if err != nil {
		t.Fatalf("failed to create temp export dir: %v", err)
	}
	defer os.RemoveAll(tmpExportDir)

	exportPath := filepath.Join(tmpExportDir, "export.jsonl")

	// 4. Test Export
	count, err := store.ExportJSONL(ctx, exportPath, "", nil)
	if err != nil {
		t.Fatalf("ExportJSONL failed: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected 2 records exported, got %d", count)
	}

	// 5. Verify File Contents
	data, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("Failed to read exported file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Errorf("Expected 2 JSONL lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], `"key":"export-`) || !strings.Contains(lines[1], `"key":"export-`) {
		t.Errorf("JSONL lines do not contain expected keys: %v", lines)
	}

	// 6. Test O_EXCL (Overwrite Protection)
	_, err = store.ExportJSONL(ctx, exportPath, "", nil)
	if err == nil {
		t.Errorf("Expected ExportJSONL to fail when targeting an existing file (O_EXCL constraint), but it succeeded")
	}
}

func TestMemoryStore_ImportJSONL(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "recall-portability-import-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewMemoryStore(context.Background(), tmpDir, "", 0, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed to create memory store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// 1. Create a dummy JSONL file with 150 records to test the 100-batch flush
	importPath := filepath.Join(tmpDir, "import.jsonl")
	f, err := os.Create(importPath)
	if err != nil {
		t.Fatalf("failed to create temp import jsonl: %v", err)
	}
	for i := range 150 {
		line := fmt.Sprintf(`{"key":"import-%d","content":"val-%d","category":"test"}`+"\n", i, i)
		f.WriteString(line)
	}
	// Add one malformed line to test resilience
	f.WriteString(`{"key":"bad-json", "content": }` + "\n")
	f.Close()

	// 2. Test Import
	stored, errs, err := store.ImportJSONL(ctx, importPath, "")
	if err != nil {
		t.Fatalf("ImportJSONL completely failed: %v", err)
	}

	if stored != 150 {
		t.Errorf("Expected 150 structurally sound records stored, got %d", stored)
	}
	if len(errs) != 1 {
		t.Errorf("Expected exactly 1 BatchError for the malformed JSON line, got %d", len(errs))
	}

	// 3. Verify store contents
	rec, err := store.Get(ctx, "import-149")
	if err != nil || rec.Content != "val-149" {
		t.Errorf("Failed to retrieve imported record via Get: %v", err)
	}
}
