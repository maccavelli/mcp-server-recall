package memory

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	badger "github.com/dgraph-io/badger/v4"
	"github.com/maccavelli/mcp-server-recall/internal/config"
)

func newTestStore(t *testing.T) (*MemoryStore, context.Context) {
	t.Helper()
	ctx := context.Background()
	store, err := NewMemoryStore(ctx, t.TempDir(), "", 100, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, ctx
}

// TestIndexSchema_CategoryWithSeparatorRoundTrips is the 0006-MADR defect-1
// reproduction. Under the old ':'-delimited schema ListCategories split the key
// on ':' and took field 2, so "team:platform" was reported as "team".
func TestIndexSchema_CategoryWithSeparatorRoundTrips(t *testing.T) {
	store, ctx := newTestStore(t)

	for _, cat := range []string{"team:platform", "a:b:c:d", "plain", "pkg:x"} {
		key := "k-" + cat
		if _, err := store.Save(ctx, "t", key, "body", cat, nil, DomainMemories, 0); err != nil {
			t.Fatalf("save %q: %v", cat, err)
		}
	}

	cats, err := store.ListCategories(ctx, DomainMemories)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("categories: %v", cats)
	for _, want := range []string{"team:platform", "a:b:c:d", "plain", "pkg:x"} {
		if cats[strings.ToLower(want)] != 1 {
			t.Errorf("category %q not round-tripped intact; got %v", want, cats)
		}
	}
}

// TestIndexSchema_TimeOrdering pins that reverse-chronological order is a
// property of the encoding. The old variable-width hex held only while every
// value was 16 digits — true from roughly 2006 to 2554 and false either side.
func TestIndexSchema_TimeOrdering(t *testing.T) {
	store, ctx := newTestStore(t)

	type seed struct {
		key string
		at  time.Time
	}
	seeds := []seed{
		{"y1970", time.Unix(0, 108000000000000)},
		{"y2006", time.Unix(0, 1152939600000000000)},
		{"y2026", time.Unix(0, 1788066000000000000)},
		{"y2554", time.Date(2554, 7, 21, 0, 0, 0, 0, time.UTC)},
	}
	for _, sd := range seeds {
		if _, _, err := store.SaveBatch(ctx, []BatchEntry{{
			Key: sd.key, Value: "body", Category: "C",
			Domain: DomainMemories, UpdatedAt: sd.at,
		}}); err != nil {
			t.Fatalf("seed %s: %v", sd.key, err)
		}
	}

	got, err := store.GetRecent(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, r := range got {
		order = append(order, r.Key)
	}
	t.Logf("GetRecent order: %v", order)
	want := []string{"y2554", "y2026", "y2006", "y1970"}
	if len(order) != len(want) {
		t.Fatalf("got %d records, want %d: %v", len(order), len(want), order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("position %d: got %q, want %q (full order %v)", i, order[i], want[i], order)
		}
	}
}

// TestIndexSchema_NoStrayNULBytes asserts the invariant the whole schema rests
// on: within an index key, 0x00 appears only as a separator, so splitting on it
// is unambiguous.
func TestIndexSchema_NoStrayNULBytes(t *testing.T) {
	store, ctx := newTestStore(t)

	if _, err := store.Save(ctx, "t", "pkg:a:b", "body", "team:platform",
		[]string{"project:x", "implements:io.Reader", "日本語"}, DomainMemories, 0); err != nil {
		t.Fatal(err)
	}

	var checked int
	_ = store.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			k := it.Item().KeyCopy(nil)
			if !isIndexKey(k) {
				continue
			}
			checked++
			if n := bytes.Count(k, []byte{indexSep}); n != 4 {
				t.Errorf("index key %q has %d NUL bytes, want exactly 4 separators", k, n)
			}
			if _, _, _, _, ok := decodeIndexKey(k); !ok {
				t.Errorf("index key %q failed to decode", k)
			}
		}
		return nil
	})
	if checked == 0 {
		t.Fatal("no index keys scanned — assertion would be vacuous")
	}
	t.Logf("verified %d index keys", checked)
}

// TestIndexSchema_DomainTeardown pins that dropping a domain leaves no index
// entry behind, which the domain-first layout makes a single prefix sweep.
func TestIndexSchema_DomainTeardown(t *testing.T) {
	store, ctx := newTestStore(t)

	for _, d := range []string{DomainMemories, DomainStandards, DomainProjects} {
		for i := range 3 {
			key := d + "-" + string(rune('a'+i))
			if _, err := store.Save(ctx, "t", key, "body", "C", []string{"shared"}, d, 0); err != nil {
				t.Fatal(err)
			}
		}
	}

	countPrefix := func(p []byte) int {
		n := 0
		_ = store.db.View(func(txn *badger.Txn) error {
			opts := badger.DefaultIteratorOptions
			opts.PrefetchValues = false
			it := txn.NewIterator(opts)
			defer it.Close()
			for it.Seek(p); it.ValidForPrefix(p); it.Next() {
				n++
			}
			return nil
		})
		return n
	}

	before := countPrefix(indexPrefix(DomainStandards, 0, ""))
	if before == 0 {
		t.Fatal("no standards index entries seeded")
	}
	if _, err := store.PurgeDomain(ctx, DomainStandards); err != nil {
		t.Fatal(err)
	}
	if after := countPrefix(indexPrefix(DomainStandards, 0, "")); after != 0 {
		t.Errorf("purge left %d orphaned index entries (had %d)", after, before)
	}
	// Other domains untouched.
	if n := countPrefix(indexPrefix(DomainMemories, 0, "")); n == 0 {
		t.Error("purging standards removed the memories index")
	}
}

// TestIndexSchema_AdversarialRoundTrip drives adversarial values through the
// full lifecycle rather than the codec alone.
func TestIndexSchema_AdversarialRoundTrip(t *testing.T) {
	store, ctx := newTestStore(t)

	cases := []struct{ key, category, tag string }{
		{"pkg:a:b", "team:platform", "project:x"},
		{"_x-looking-key", "_x", "_x"},
		{"with space", "with space", "with space"},
		{"unicode-日本語", "категория", "тег"},
		{"pct-100%", "100%", "50%"},
	}
	for _, c := range cases {
		if _, err := store.Save(ctx, "t", c.key, "content "+c.key, c.category, []string{c.tag}, DomainMemories, 0); err != nil {
			t.Fatalf("save %+v: %v", c, err)
		}
	}
	for _, c := range cases {
		rec, err := store.Get(ctx, c.key)
		if err != nil {
			t.Errorf("get %q: %v", c.key, err)
			continue
		}
		if rec.Category != c.category {
			t.Errorf("category for %q: got %q want %q", c.key, rec.Category, c.category)
		}
	}
	cats, err := store.ListCategories(ctx, DomainMemories)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if cats[strings.ToLower(c.category)] == 0 {
			t.Errorf("category %q missing from listing %v", c.category, cats)
		}
	}
	for _, c := range cases {
		if _, err := store.DeleteByCategory(ctx, c.category); err != nil {
			t.Errorf("delete by category %q: %v", c.category, err)
		}
	}
	cats, err = store.ListCategories(ctx, DomainMemories)
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 0 {
		t.Errorf("categories remain after delete: %v", cats)
	}
}
