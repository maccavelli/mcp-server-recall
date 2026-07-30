package memory

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/mcp-server-recall/internal/config"

	"github.com/dgraph-io/badger/v4"
)

func TestMemoryStore_VacuumSessions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "recall-vacuum-sessions-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewMemoryStore(context.Background(), tmpDir, "", 0, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	now := time.Now()

	// Create some sessions
	// 1. Success session, 10 days old
	s1Key := "session:1"
	s1 := &Record{
		Content:   "session 1 content",
		Domain:    DomainSessions,
		Category:  "test",
		UpdatedAt: now.AddDate(0, 0, -10),
	}
	data1, _ := marshalRecord(s1)
	_ = store.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(s1Key), data1)
	})
	// Wait, I should use store.Save if possible, but Save doesn't allow setting UpdateAt easily.
	// Actually, I can just write to Badger directly since this is a unit test.
	// But I need to use the right indices.

	// Let's use a helper to inject records with specific timestamps.
	injectRecord := func(key string, rec *Record) {
		data, _ := marshalRecord(rec)
		_ = store.db.Update(func(txn *badger.Txn) error {
			_ = txn.Set([]byte(key), data)
			// Add domain index
			idxKey := fmt.Sprintf("_idx:domain:%s:%s", rec.Domain, key)
			_ = txn.Set([]byte(idxKey), []byte(key))
			// Add tag indices
			for _, tag := range rec.Tags {
				tagKey := fmt.Sprintf("_idx:tag:%s:%s", strings.ToLower(tag), key)
				_ = txn.Set([]byte(tagKey), []byte(key))
			}
			return nil
		})
	}

	injectRecord(s1Key, s1)

	// 2. Success session, 2 days old
	s2Key := "session:2"
	s2 := &Record{
		Content:   "session 2 content",
		Domain:    DomainSessions,
		Category:  "test",
		UpdatedAt: now.AddDate(0, 0, -2),
	}
	injectRecord(s2Key, s2)

	// 3. Error session, 10 days old
	s3Key := "session:3"
	s3 := &Record{
		Content:   "session 3 content",
		Domain:    DomainSessions,
		Category:  "test",
		UpdatedAt: now.AddDate(0, 0, -10),
		Tags:      []string{"outcome:error"},
	}
	injectRecord(s3Key, s3)

	// Vacuum sessions with outcome:error, 5 days old
	count, err := store.VacuumSessions(ctx, "", "error", 0, 5)
	if err != nil {
		t.Fatalf("VacuumSessions failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 vacuumed session, got %d", count)
	}

	// Verify s3 is tombstoned
	rec3, _ := store.Get(ctx, s3Key)
	if !strings.Contains(rec3.Content, "tombstoned") {
		t.Errorf("expected s3 to be tombstoned, got content: %s", rec3.Content)
	}

	// Verify s1 is NOT vacuumed (outcome doesn't match)
	rec1, _ := store.Get(ctx, s1Key)
	if strings.Contains(rec1.Content, "tombstoned") {
		t.Errorf("expected s1 NOT to be tombstoned")
	}
}

func TestMemoryStore_VacuumMemories(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "recall-vacuum-memories-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewMemoryStore(context.Background(), tmpDir, "", 0, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	now := time.Now()

	injectRecord := func(key string, rec *Record) {
		data, _ := marshalRecord(rec)
		_ = store.db.Update(func(txn *badger.Txn) error {
			_ = txn.Set([]byte(key), data)
			idxKey := fmt.Sprintf("_idx:domain:%s:%s", rec.Domain, key)
			_ = txn.Set([]byte(idxKey), []byte(key))
			return nil
		})
	}

	// Create memories
	// 1. Old memory
	m1Key := "mem:1"
	injectRecord(m1Key, &Record{
		Content:   "old memory",
		Domain:    DomainMemories,
		Category:  "test",
		UpdatedAt: now.AddDate(0, 0, -40),
	})

	// 2. Duplicate memories
	m2Key := "mem:2"
	injectRecord(m2Key, &Record{
		Content:   "duplicate content",
		Domain:    DomainMemories,
		Category:  "dedup",
		UpdatedAt: now.AddDate(0, 0, -5),
	})
	m3Key := "mem:3"
	injectRecord(m3Key, &Record{
		Content:   "duplicate content",
		Domain:    DomainMemories,
		Category:  "dedup",
		UpdatedAt: now.AddDate(0, 0, -4),
	})

	// Vacuum memories > 30 days and dedup > 0.9
	report, err := store.VacuumMemories(ctx, 0.9, "", false)
	if err != nil {
		t.Fatalf("VacuumMemories failed: %v", err)
	}
	if report.Merged != 1 {
		t.Errorf("expected 1 merged memory, got %d", report.Merged)
	}
}

func TestMemoryStore_VacuumStandards(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "recall-vacuum-standards-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewMemoryStore(context.Background(), tmpDir, "", 0, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	injectRecord := func(key string, rec *Record) {
		data, _ := marshalRecord(rec)
		_ = store.db.Update(func(txn *badger.Txn) error {
			_ = txn.Set([]byte(key), data)
			idxKey := fmt.Sprintf("_idx:domain:%s:%s", rec.Domain, key)
			_ = txn.Set([]byte(idxKey), []byte(key))
			return nil
		})
	}

	s1Key := "pkg:test:1"
	injectRecord(s1Key, &Record{
		Content:  "standard 1",
		Domain:   DomainStandards,
		Category: "HarvestedCode",
	})

	// Create another one that is NOT standards
	m1Key := "mem:1"
	injectRecord(m1Key, &Record{
		Content:  "memory 1",
		Domain:   DomainMemories,
		Category: "test",
	})

	// Vacuum standards (report only)
	report, err := store.VacuumStandards(ctx, true)
	if err != nil {
		t.Fatalf("VacuumStandards failed: %v", err)
	}
	if report.TotalScanned < 1 {
		t.Errorf("expected at least 1 scanned standard, got %d", report.TotalScanned)
	}
}
