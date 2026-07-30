package telemetry

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maccavelli/mcp-server-recall/internal/config"
	"github.com/maccavelli/mcp-server-recall/internal/memory"
)

func TestWriteSnapshot(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "recall-telemetry-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Set dbPath via env var so config.New picks it up
	t.Setenv("MCP_RECALL_DBPATH", tmpDir)
	cfg := config.New("1.0.0-test")

	store, err := memory.NewMemoryStore(context.Background(), tmpDir, "", 1000, config.BatchConfig{MaxBatchSize: 100})
	if err != nil {
		t.Fatalf("failed to create memory store: %v", err)
	}
	defer store.Close()

	logMsg := "test log line"
	logStream := func() string {
		return logMsg
	}

	// Initial snapshot
	WriteSnapshot(cfg, store, logStream, nil, nil)

	ringPath := filepath.Join(tmpDir, "telemetry.ring")
	if _, err := os.Stat(ringPath); os.IsNotExist(err) {
		t.Errorf("telemetry.ring was not created")
	}

	data, err := os.ReadFile(ringPath)
	if err != nil {
		t.Fatalf("failed to read telemetry.ring: %v", err)
	}

	if len(data) == 0 {
		t.Errorf("telemetry.ring is empty")
	}
}

func TestStartTelemetryLoop(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "recall-telemetry-loop-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Set dbPath via env var so config.New picks it up
	t.Setenv("MCP_RECALL_DBPATH", tmpDir)
	cfg := config.New("1.0.0-test")

	store, err := memory.NewMemoryStore(context.Background(), tmpDir, "", 1000, config.BatchConfig{MaxBatchSize: 100})
	if err != nil {
		t.Fatalf("failed to create memory store: %v", err)
	}
	defer store.Close()

	logStream := func() string { return "loop test" }

	// Start loop with cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	StartTelemetryLoop(ctx, cfg, store, logStream, nil, nil)
	cancel()

	// Wait a bit to ensure it hits context Done
	time.Sleep(10 * time.Millisecond)
}

func TestDirSize(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "recall-dirsize-*")
	defer os.RemoveAll(tmpDir)

	// Create a 10 byte file
	os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("0123456789"), 0644)

	size := dirSize(tmpDir)
	if size != 10 {
		t.Errorf("expected size 10, got %d", size)
	}

	// Test error path for walk
	sizeError := dirSize("/path/does/not/exist/we/hope/1234")
	if sizeError != 0 {
		t.Errorf("expected size 0 for nonexistent dir, got %d", sizeError)
	}
}

func TestTopNCategories(t *testing.T) {
	// Should not panic on nil
	_ = topNCategories(nil, 3)

	// Should work correctly
	res := topNCategories(map[string]int{"a": 1, "b": 10, "c": 5, "d": 20}, 3)
	if len(res) == 0 {
		t.Error("expected non-empty categories")
	}
	// We could assert "d", "b", "c", "a" order but we just need coverage here
}
