package memory

import (
	"context"
	"testing"

	"github.com/maccavelli/mcp-server-recall/internal/config"
)

// TestOverviewListsSaveToRecallRecords is the 0005-PLAN Phase 7 acceptance test:
// a record written the way save_to_recall writes one (arbitrary category, non-pkg
// key) must appear in the standards overview. It did not before the rewrite.
func TestOverviewListsSaveToRecallRecords(t *testing.T) {
	ctx := context.Background()
	store, err := NewMemoryStore(ctx, t.TempDir(), "", 100, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer store.Close()

	// Exactly the shape handleSaveToRecall produces.
	if _, err := store.Save(ctx, "Standard", "srv:standards:1730000000", "payload", "Testing", nil, DomainStandards, 0); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := store.Save(ctx, "Project", "srv:projects:1730000001", "payload", "Testing", nil, DomainProjects, 0); err != nil {
		t.Fatalf("save: %v", err)
	}

	groups, err := store.ListStandardsOverview(ctx, "")
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	grp, ok := groups["Testing"]
	if !ok {
		t.Fatalf("category %q missing from standards overview; got %v", "Testing", keysOf(groups))
	}
	if grp.TotalRecords != 1 || len(grp.Records) != 1 {
		t.Fatalf("expected 1 record, got %d/%d", grp.TotalRecords, len(grp.Records))
	}
	if grp.Records[0].Key != "srv:standards:1730000000" {
		t.Errorf("unexpected key %q", grp.Records[0].Key)
	}

	pgroups, err := store.ListDomainOverview(ctx, DomainProjects, "")
	if err != nil {
		t.Fatalf("projects overview: %v", err)
	}
	if _, ok := pgroups["Testing"]; !ok {
		t.Fatalf("projects overview missing category; got %v", keysOf(pgroups))
	}
	// Domain isolation: the standards record must not appear under projects.
	if len(pgroups["Testing"].Records) != 1 || pgroups["Testing"].Records[0].Key != "srv:projects:1730000001" {
		t.Errorf("domain leak in projects overview: %+v", pgroups["Testing"].Records)
	}
}

func keysOf(m map[string]*DomainCategoryOverview) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}
