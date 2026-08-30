package memory

import (
	"context"
	"testing"

	"github.com/maccavelli/mcp-server-recall/internal/config"
	"github.com/maccavelli/mcp-server-recall/internal/search"
)

// foreignDomains are the namespaces whose records must never surface from a
// memory-scoped read path.
var foreignDomains = []string{
	DomainSessions, DomainStandards, DomainProjects,
	DomainDialecticHistory, DomainDocuments, DomainEcosystem,
}

func seedCrossDomain(ctx context.Context, t *testing.T, store *MemoryStore, tag string) {
	t.Helper()
	if _, err := store.Save(ctx, "mem", "mem-1", "memory content alpha", "Note", []string{tag}, DomainMemories, 0); err != nil {
		t.Fatalf("seed memories: %v", err)
	}
	for _, d := range foreignDomains {
		if _, err := store.Save(ctx, "foreign", "foreign-"+d, "foreign content alpha", "Foreign", []string{tag}, d, 0); err != nil {
			t.Fatalf("seed %s: %v", d, err)
		}
	}
}

// TestSearch_TagFilterIsDomainScoped pins that a tag shared across namespaces
// cannot pull foreign records into memory-scoped search. The tag index is not
// domain-partitioned, so isolation depends entirely on the per-record domain
// check in searchByTag and the domain: required-tag on the Bleve path.
func TestSearch_TagFilterIsDomainScoped(t *testing.T) {
	ctx := context.Background()
	store, err := NewMemoryStore(ctx, t.TempDir(), "", 100, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer store.Close()

	const tag = "shared"
	seedCrossDomain(ctx, t, store, tag)

	for _, tc := range []struct{ name, query string }{
		{"badger fallback (no query)", ""},
		{"with query", "alpha"},
	} {
		seq, err := store.Search(ctx, tc.query, tag, 0)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		for r := range seq {
			if r.Key != "mem-1" {
				t.Errorf("%s: foreign-domain key %q leaked into memory search", tc.name, r.Key)
			}
		}
	}
}

// TestSearchScoped_BleveIsDomainScoped exercises the Bleve fast path directly.
// Without a search engine attached, MemoryStore.Search silently falls back to
// Badger iteration, so an end-to-end test alone never proves Bleve scoping.
func TestSearchScoped_BleveIsDomainScoped(t *testing.T) {
	ctx := context.Background()
	store, err := NewMemoryStore(ctx, t.TempDir(), "", 100, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer store.Close()

	eng, err := search.InitStorage(t.TempDir())
	if err != nil {
		t.Fatalf("InitStorage: %v", err)
	}
	if err := store.SetSearchEngine(ctx, eng); err != nil {
		t.Fatalf("SetSearchEngine: %v", err)
	}

	const tag = "shared"
	seedCrossDomain(ctx, t, store, tag)

	hits, err := store.search.SearchScoped(ctx, "alpha", nil, []string{"domain:" + DomainMemories, tag}, 0)
	if err != nil {
		t.Fatalf("SearchScoped: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("no Bleve hits — scoping assertion would be vacuous")
	}
	for _, h := range hits {
		if h.ID != "mem-1" {
			t.Errorf("foreign-domain key %q leaked from a domain:memories query", h.ID)
		}
	}
}

// TestListCategories_DomainScoping pins both contracts of ListCategories: the
// memory-scoped form backing the "Memory Categories" listing, and the
// whole-datastore form backing the telemetry dashboard.
func TestListCategories_DomainScoping(t *testing.T) {
	ctx := context.Background()
	store, err := NewMemoryStore(ctx, t.TempDir(), "", 100, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer store.Close()

	seedCrossDomain(ctx, t, store, "shared")

	// Category names come back lowercased: they are parsed out of the
	// _idx:cat:<lowercased category>:<key> index key, not the record.
	scoped, err := store.ListCategories(ctx, DomainMemories)
	if err != nil {
		t.Fatalf("scoped: %v", err)
	}
	if _, ok := scoped["foreign"]; ok {
		t.Errorf(`"foreign" is owned by other domains but appeared in memory categories: %v`, scoped)
	}
	if scoped["note"] != 1 {
		t.Errorf("expected the memories category once, got %v", scoped)
	}

	all, err := store.ListCategories(ctx, "")
	if err != nil {
		t.Fatalf("global: %v", err)
	}
	if all["foreign"] != len(foreignDomains) {
		t.Errorf("global distribution should count every domain, got %v", all)
	}
	if all["note"] != 1 {
		t.Errorf("global distribution lost the memories category: %v", all)
	}
}
