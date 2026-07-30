package memory

import (
	"context"
	"os"
	"testing"

	"github.com/maccavelli/mcp-server-recall/internal/config"
)

func TestMemoryStore_DomainManagement(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "recall-domain-mgmt-*")
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

	// 1. Inject some domain records
	_, _ = store.Save(ctx, "session:1", "sess1", "session 1 data", "sessCat", nil, DomainSessions, 0)
	_, _ = store.Save(ctx, "session:2", "sess2", "session 2 data", "sessCat", nil, DomainSessions, 0)
	_, _ = store.Save(ctx, "std:1", "std1", "standard data 1", "SysDrift", nil, DomainStandards, 0)
	_, _ = store.Save(ctx, "proj:1", "proj1", "project data 1", "HarvestedCode", nil, DomainProjects, 0)

	// Verify they exist
	if _, err := store.Get(ctx, "sess1"); err != nil {
		t.Fatalf("expected sess1 to exist")
	}

	// 2. Test DeleteDomain (sessions)
	deleted, err := store.DeleteDomain(ctx, DomainSessions)
	if err != nil {
		t.Fatalf("DeleteDomain failed: %v", err)
	}
	if deleted != 2 {
		t.Errorf("expected 2 deleted sessions, got %d", deleted)
	}

	if _, err := store.Get(ctx, "sess1"); err == nil {
		t.Errorf("expected sess1 to be deleted")
	}

	// 3. Test DeleteStandards
	deletedStds, err := store.DeleteStandards(ctx, "SysDrift", "")
	if err != nil {
		t.Fatalf("DeleteStandards failed: %v", err)
	}
	if deletedStds != 1 {
		t.Errorf("expected 1 deleted standard, got %d", deletedStds)
	}

	// 4. Test DeleteProjects
	_, _ = store.Save(ctx, "proj:2", "proj2", "project data 2", "HarvestedCode", nil, DomainProjects, 0)
	deletedProjs, err := store.DeleteProjects(ctx, "HarvestedCode", "")
	if err != nil {
		t.Fatalf("DeleteProjects failed: %v", err)
	}
	if deletedProjs != 2 { // proj1 and proj2
		t.Errorf("expected 2 deleted projects, got %d", deletedProjs)
	}

	// 5. Test PurgeDomain
	_, _ = store.Save(ctx, "std:2", "std2", "standard data 2", "HarvestedCode", nil, DomainStandards, 0)
	purged, err := store.PurgeDomain(ctx, DomainStandards)
	if err != nil {
		t.Fatalf("PurgeDomain failed: %v", err)
	}
	if purged != 1 {
		t.Errorf("expected 1 purged standard, got %d", purged)
	}

	// 6. Test PruneDomain
	_, _ = store.Save(ctx, "std:3", "std3", "standard data 3", "HarvestedCode", nil, DomainStandards, 0)
	pruned, err := store.PruneDomain(ctx, DomainStandards, 0)
	if err != nil {
		t.Fatalf("PruneDomain failed: %v", err)
	}
	if pruned != 1 {
		t.Errorf("expected 1 pruned standard, got %d", pruned)
	}
}

func TestMemoryStore_DomainOverviewAndSearch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "recall-domain-search-*")
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

	// Add standards
	_, _ = store.Save(ctx, "std1 title", "pkg:internal/foo:Symbol", "standard one content", "HarvestedCode", nil, DomainStandards, 0)
	_, _ = store.Save(ctx, "std2 title", "pkg:internal/foo:Doc", "package docs", "PackageDoc", nil, DomainStandards, 0)
	_, _ = store.Save(ctx, "std3 title", "pkg:internal/bar:Drift", "sys drift", "SysDrift", nil, DomainStandards, 0)

	// Test ListStandardsOverview
	overview, err := store.ListStandardsOverview(ctx, "")
	if err != nil {
		t.Fatalf("ListStandardsOverview failed: %v", err)
	}
	if len(overview) == 0 {
		t.Errorf("expected non-empty overview")
	}

	// Test SearchStandards
	resultsSeq, err := store.SearchStandards(ctx, SearchDomainQuery{
		Query: "content",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("SearchStandards failed: %v", err)
	}
	var results []*SearchResult
	for r := range resultsSeq {
		results = append(results, r)
	}
	if len(results) == 0 {
		t.Errorf("expected search results")
	}

	// Test ListDomainOverview
	domainOverview, err := store.ListDomainOverview(ctx, DomainStandards, "")
	if err != nil {
		t.Fatalf("ListDomainOverview failed: %v", err)
	}
	if len(domainOverview) == 0 {
		t.Errorf("expected non-empty domain overview")
	}

	// Test SearchDomain
	domainResultsSeq, err := store.SearchDomain(ctx, SearchDomainQuery{
		TargetDomain: DomainStandards,
		Query:        "content",
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("SearchDomain failed: %v", err)
	}
	var domainResults []*SearchResult
	for r := range domainResultsSeq {
		domainResults = append(domainResults, r)
	}
	if len(domainResults) == 0 {
		t.Errorf("expected domain search results")
	}
}
