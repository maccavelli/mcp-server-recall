package memory

import (
	"bytes"
	"context"
	"strings"
	"testing"

	badger "github.com/dgraph-io/badger/v4"
	"github.com/maccavelli/mcp-server-recall/internal/config"
)

// TestIndexWrite_EntryAccountingAndNUL pins the write-side contract of the
// 0006-MADR schema: a record with N tags produces exactly N+2 entries (one per
// tag, one time, one category), every entry has an empty value, no legacy
// _idx: entry is written, and a NUL in any component is rejected before any
// write occurs.
func TestIndexWrite_EntryAccountingAndNUL(t *testing.T) {
	ctx := context.Background()
	store, err := NewMemoryStore(ctx, t.TempDir(), "", 100, config.New("test").BatchSettings())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	tags := []string{"a", "b", "c"}
	if _, err := store.Save(ctx, "t", "k1", "body", "Cat", tags, DomainMemories, 0); err != nil {
		t.Fatal(err)
	}

	var idx, oldIdx, empties int
	_ = store.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			k := it.Item().Key()
			if bytes.HasPrefix(k, []byte("_idx:")) {
				oldIdx++
			}
			if isIndexKey(k) {
				idx++
				if it.Item().ValueSize() == 0 {
					empties++
				}
			}
		}
		return nil
	})
	wantN := len(tags) + 2 // N tags + time + category
	t.Logf("new-schema entries=%d (want %d), empty-valued=%d, old-schema entries=%d", idx, wantN, empties, oldIdx)
	if idx != wantN {
		t.Errorf("entry count %d, want N+2=%d", idx, wantN)
	}
	if empties != idx {
		t.Errorf("%d of %d entries have non-empty values", idx-empties, idx)
	}
	if oldIdx != 0 {
		t.Errorf("%d legacy _idx: entries still written", oldIdx)
	}

	// NUL rejection, single and batch
	if _, err := store.Save(ctx, "t", "bad\x00key", "b", "C", nil, DomainMemories, 0); err == nil {
		t.Error("Save accepted a NUL in the key")
	} else if !strings.Contains(err.Error(), "NUL") {
		t.Errorf("unclear error: %v", err)
	}
	if _, _, err := store.SaveBatch(ctx, []BatchEntry{
		{Key: "ok", Value: "v", Category: "C", Domain: DomainMemories},
		{Key: "bad", Value: "v", Category: "C\x00X", Domain: DomainMemories},
	}); err == nil {
		t.Error("SaveBatch accepted a NUL in a category")
	}
	// The valid entry from the rejected batch must not have been written.
	if _, gErr := store.Get(ctx, "ok"); gErr == nil {
		t.Error("rejected batch wrote a partial result")
	}
}
