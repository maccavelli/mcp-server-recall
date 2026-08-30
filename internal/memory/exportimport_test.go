package memory

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/mcp-server-recall/internal/config"
)

// TestExportImportRoundTrip exercises the documented upgrade path from
// 0006-MADR end to end: export from one store, import into a fresh one, and
// compare. Since the index schema change is not backwards compatible, this is
// the only supported route for carrying data across it, so it is pinned rather
// than assumed.
func TestExportImportRoundTrip(t *testing.T) {
	ctx := context.Background()
	dirA := t.TempDir()
	storeA, err := NewMemoryStore(ctx, dirA, "", 100, config.New("test").BatchSettings())
	if err != nil {
		t.Fatal(err)
	}

	type rec struct {
		key, cat, dom string
		tags          []string
	}
	seeds := []rec{
		{"mem-1", "journal", DomainMemories, []string{"project:alpha"}},
		{"team:platform:doc", "team:platform", DomainMemories, []string{"implements:io.Reader"}},
		{"srv:standards:1", "Logging", DomainStandards, []string{"project:alpha"}},
		{"srv:projects:1", "Roadmap", DomainProjects, nil},
		{"srv:session:1", "runtime", DomainSessions, []string{"outcome:ok"}},
	}
	for _, s := range seeds {
		if _, err := storeA.Save(ctx, "title-"+s.key, s.key, "content of "+s.key, s.cat, s.tags, s.dom, 0); err != nil {
			t.Fatalf("seed %s: %v", s.key, err)
		}
	}

	out := filepath.Join(t.TempDir(), "backup.jsonl")
	exported, err := storeA.ExportJSONL(ctx, out, "", nil)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	t.Logf("exported %d records", exported)
	_ = storeA.Close()

	fi, _ := os.Stat(out)
	t.Logf("exported %d bytes", fi.Size())

	// Fresh store, as an operator gets after purging.
	storeB, err := NewMemoryStore(ctx, t.TempDir(), "", 100, config.New("test").BatchSettings())
	if err != nil {
		t.Fatal(err)
	}
	defer storeB.Close()

	n, importErrs, err := storeB.ImportJSONL(ctx, out, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	for _, e := range importErrs {
		t.Errorf("import error on %s: %v", e.Key, e.Error)
	}
	t.Logf("imported %d records", n)
	if n != len(seeds) {
		t.Errorf("imported %d, want %d", n, len(seeds))
	}

	for _, s := range seeds {
		got, gErr := storeB.Get(ctx, s.key)
		if gErr != nil {
			t.Errorf("missing after import: %s (%v)", s.key, gErr)
			continue
		}
		if got.Category != s.cat {
			t.Errorf("%s category: got %q want %q", s.key, got.Category, s.cat)
		}
		if got.Domain != s.dom {
			t.Errorf("%s domain: got %q want %q", s.key, got.Domain, s.dom)
		}
	}

	// Indexes must be rebuilt by the import, not just the records.
	cats, err := storeB.ListCategories(ctx, DomainMemories)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("memories categories after import: %v", cats)
	if cats["team:platform"] != 1 {
		t.Errorf("colon category not indexed after import: %v", cats)
	}
	groups, err := storeB.ListStandardsOverview(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := groups["Logging"]; !ok {
		t.Errorf("standards overview empty after import: %v", groups)
	}
}
