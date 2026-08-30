package memory

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/maccavelli/mcp-server-recall/internal/config"
	"github.com/maccavelli/mcp-server-recall/internal/search"

	"github.com/dgraph-io/badger/v4"
)

func TestVacuumProjects(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := NewMemoryStore(ctx, t.TempDir(), "", 100, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed to create memory store: %v", err)
	}
	defer db.Close()

	// Add some projects
	_, _ = db.Save(ctx, "proj1-title", "proj1", "project content 1", "test-cat", nil, DomainProjects, 0.9)
	_, _ = db.Save(ctx, "proj1-dup", "proj1", "project content 1 dup", "test-cat", nil, DomainProjects, 0.9) // Duplicate
	_, _ = db.Save(ctx, "proj2", "proj2", "project content 2", "test-cat", nil, DomainProjects, 0.9)

	// Run Vacuum
	report, err := db.VacuumProjects(ctx, false)
	if err != nil {
		t.Fatalf("VacuumProjects failed: %v", err)
	}
	if report == nil {
		t.Fatal("expected report to not be nil")
	}
}

func TestGetTopQueries(t *testing.T) {
	store, err := NewMemoryStore(context.Background(), t.TempDir(), "", 100, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Record some search telemetry
	store.RecordSearchTelemetry("test1", 100)
	store.RecordSearchTelemetry("test1", 200)
	store.RecordSearchTelemetry("test2", 150)
	for i := 0; i < 10; i++ {
		store.RecordSearchTelemetry("spam", 5)
	}

	top := store.GetTopQueries(2)
	if len(top) != 2 {
		t.Fatalf("expected 2 top queries, got %d", len(top))
	}

	// "spam" should be the highest since we logged it 10 times
	if top[0].Query != "spam" {
		t.Errorf("expected spam as top query, got %s", top[0].Query)
	}
	// average latency should be 5 for spam
	if top[0].AvgLatencyMs != 5 {
		t.Errorf("expected spam avg latency 5, got %v", top[0].AvgLatencyMs)
	}
}

func TestSearchByTag(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := NewMemoryStore(ctx, t.TempDir(), "", 100, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed to create memory store: %v", err)
	}
	defer db.Close()

	_, _ = db.Save(ctx, "mem1-title", "mem1", "memory content 1", "", []string{"target-tag"}, DomainMemories, 0.9)
	_, _ = db.Save(ctx, "mem2-title", "mem2", "memory content 2", "", []string{"other-tag"}, DomainMemories, 0.9)

	count := 0
	seq, err := db.Search(ctx, "", "target-tag", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	seq(func(sr *SearchResult) bool {
		if sr.Key == "mem1" {
			count++
		}
		return true
	})
	if count != 1 {
		t.Errorf("Expected 1 result for searchByTag, got %d", count)
	}
}

func TestDomainSearchHelpers(t *testing.T) {
	q := SearchDomainQuery{
		SymbolType:    "struct",
		Interface:     "Closer",
		Receiver:      "MemoryStore",
		Domain:        DomainProjects,
		PackageFilter: "memory",
		TargetDomain:  DomainMemories,
		Tags:          []string{"tag1"},
		TagMatchMode:  "all",
	}
	tags := buildSearchDomainRequiredTags(q)
	if len(tags) != 7 {
		t.Errorf("Expected 7 tags, got %d", len(tags))
	}

	rec := &Record{Tags: []string{"tag2"}}
	q2 := SearchDomainQuery{
		PackageFilter: "memory",
		KeyPrefix:     "pkg:memory:test",
		KeySuffix:     ".go",
		Tags:          []string{"tag1", "tag2"},
		TagMatchMode:  fieldAny,
	}
	if bleveHitMatchesDomainSearch(q2, "pkg:memory:test.go", rec) != true {
		t.Errorf("Expected hit to match")
	}
	if bleveHitMatchesDomainSearch(q2, "other:test.go", rec) != false {
		t.Errorf("Expected hit to fail package filter")
	}
	if bleveHitMatchesDomainSearch(q2, "pkg:memory:other.go", rec) != false {
		t.Errorf("Expected hit to fail prefix")
	}
	if bleveHitMatchesDomainSearch(q2, "pkg:memory:test.txt", rec) != false {
		t.Errorf("Expected hit to fail suffix")
	}
	rec.Tags = []string{"tag3"}
	if bleveHitMatchesDomainSearch(q2, "pkg:memory:test.go", rec) != false {
		t.Errorf("Expected hit to fail tag match")
	}
}

func TestGetByAttributesAndTTLHorizon(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := NewMemoryStore(ctx, t.TempDir(), "", 100, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed to create memory store: %v", err)
	}
	defer db.Close()

	_, _ = db.Save(ctx, "mem1-title", "mem1", "memory content 1", "cat1", []string{"tag1"}, DomainMemories, 0.9)

	// Update with session and source path to test attributes
	db.db.Update(func(txn *badger.Txn) error {
		item, _ := txn.Get([]byte("mem1"))
		item.Value(func(v []byte) error {
			rec, _ := migrateRecord(v)
			rec.SessionID = "sess-1"
			rec.SourcePath = "src/main.go"
			rec.SymbolName = "MySymbol"
			// Also add an inner JSON tag to cover that branch
			rec.Content = `{"tags": ["inner1", "inner2"]}`
			bytes, _ := marshalRecord(rec)
			return txn.Set([]byte("mem1"), bytes)
		})
		return nil
	})

	// GetByAttributes
	res, err := db.GetByAttributes(ctx, DomainMemories, &AttributeQuery{
		SessionID:  "sess-1",
		SourcePath: "src/main.go",
		Category:   "cat1",
		SymbolName: "MySymbol",
		Tags:       []string{"inner2"},
	})
	if err != nil {
		t.Fatalf("GetByAttributes failed: %v", err)
	}
	if len(res) != 1 {
		t.Errorf("Expected 1 result, got %d", len(res))
	}

	// Filters that don't match
	queries := []*AttributeQuery{
		{SessionID: "sess-2"},
		{SourcePath: "other.go"},
		{Category: "cat2"},
		{SymbolName: "OtherSymbol"},
		{Tags: []string{"nonexistent"}},
	}
	for i, q := range queries {
		res, _ = db.GetByAttributes(ctx, DomainMemories, q)
		if len(res) != 0 {
			t.Errorf("Expected 0 results for non-matching query %d, got %d", i, len(res))
		}
	}

	// Empty domain should fail
	_, err = db.GetByAttributes(ctx, "", &AttributeQuery{})
	if err == nil {
		t.Errorf("Expected error for empty domain")
	}

	// GetTTLHorizon
	h24, h7, h30 := db.GetTTLHorizon(ctx, 10)
	if h24 < 0 || h7 < 0 || h30 < 0 {
		t.Errorf("Expected positive TTL horizon")
	}
}

func TestExportJSONL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := NewMemoryStore(ctx, t.TempDir(), "", 100, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed to create memory store: %v", err)
	}
	defer db.Close()

	_, _ = db.Save(ctx, "std1-title", "std1", "standard content 1", "cat1", []string{"tag1"}, DomainStandards, 0.9)

	exportPath := filepath.Join(t.TempDir(), "export.jsonl")
	exportedCount, err := db.ExportJSONL(ctx, exportPath, "", nil)
	if err != nil {
		t.Fatalf("ExportJSONL failed: %v", err)
	}
	if exportedCount != 1 {
		t.Errorf("Expected 1 exported record, got %d", exportedCount)
	}

	size, _ := db.GetDBSize()
	if size < 0 {
		t.Errorf("Expected non-negative DB size")
	}

	_, _, _, _, _, _ = db.GetExtendedTelemetry()
	_, _, _ = db.GetWriteOps()
}

func TestSearchSessions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tmpDir := t.TempDir()
	db, err := NewMemoryStore(ctx, tmpDir, "", 100, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed to create memory store: %v", err)
	}
	defer db.Close()

	engine, _ := search.InitStorage(t.TempDir())
	_ = db.SetSearchEngine(ctx, engine)

	_, _ = db.Save(ctx, "session1-title", "sess1", "session content 1", "cat1", []string{"tag1", "project:proj1", "outcome:success", "trace:ctx1"}, DomainSessions, 0.9)
	_, _ = db.Save(ctx, "session2-title", "sess2", "session content 2", "cat2", []string{"tag2", "project:proj2", "outcome:fail", "trace:ctx2"}, DomainSessions, 0.9)

	db.SyncSearchIndex(ctx)

	// 1. With query (uses Bleve) and all parameters
	res, err := db.SearchSessions(ctx, DomainSessions, "session content", "proj1", "srv1", "success", "ctx1", 1)
	if err != nil {
		t.Fatalf("SearchSessions failed: %v", err)
	}
	for range res {
		break // early abort (yield false)
	}

	// 2. Without query (uses Badger scan)
	res2, err := db.SearchSessions(ctx, DomainSessions, "", "proj1", "srv1", "success", "ctx1", 10)
	if err != nil {
		t.Fatalf("SearchSessions failed: %v", err)
	}
	for range res2 {
	}

	// 3. With query, but empty optional parameters
	res3, err := db.SearchSessions(ctx, "", "session", "", "", "", "", 10)
	require.NoError(t, err)
	for range res3 {
	}

	// 4. Without query, empty optional parameters
	res4, err := db.SearchSessions(ctx, "", "", "", "", "", "", 10)
	require.NoError(t, err)
	for range res4 {
	}

	// 5. With query, searchEngine is nil
	db.mu.Lock()
	searchOrig := db.search
	db.search = nil
	db.mu.Unlock()

	res5, err := db.SearchSessions(ctx, DomainSessions, "session", "proj1", "srv1", "success", "ctx1", 10)
	require.NoError(t, err)
	for range res5 {
	}

	db.mu.Lock()
	db.search = searchOrig
	db.mu.Unlock()
}

func TestTTLHorizonAdvanced(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := NewMemoryStore(ctx, t.TempDir(), "", 100, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed to create memory store: %v", err)
	}
	defer db.Close()

	_, _ = db.Save(ctx, "session1-title", "sess1", "session content 1", "cat1", []string{"tag1"}, DomainSessions, 0.9)

	// Just need to trigger the various branches. We don't have a way to artificially age the record
	// unless we modify the db directly, but GetTTLHorizon loops through all of them.
	// Let's test different purgeDays.
	_, _, _ = db.GetTTLHorizon(ctx, 0)
	_, _, _ = db.GetTTLHorizon(ctx, 30)
}

func TestSearchDomain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := NewMemoryStore(ctx, t.TempDir(), "", 100, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed to create memory store: %v", err)
	}
	defer db.Close()

	_, _ = db.Save(ctx, "std1-title", "std1", "standard content 1", "cat1", []string{"tag1"}, DomainStandards, 0.9)

	q := SearchDomainQuery{
		TargetDomain: DomainStandards,
		Limit:        10,
	}

	seq, err := db.SearchDomain(ctx, q)
	if err != nil {
		t.Fatalf("SearchDomain failed: %v", err)
	}

	count := 0
	seq(func(sr *SearchResult) bool {
		count++
		return true
	})

	if count != 1 {
		t.Errorf("Expected 1 result from SearchDomain, got %d", count)
	}
}

func TestExtraFunctions(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := NewMemoryStore(context.Background(), tmpDir, "", 0, config.New("test").BatchSettings())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	engine, err := search.InitStorage(t.TempDir())
	if err == nil {
		_ = db.SetSearchEngine(context.Background(), engine)
	}

	// Seed data
	_, _ = db.Save(context.Background(), DomainSessions, "session1", "val1", "cat1", nil, "session1", 0)
	_, _ = db.Save(context.Background(), DomainProjects, "proj1", "val2", "cat2", nil, "proj1", 0)
	_, _ = db.Save(context.Background(), DomainStandards, "std1", "val3", "cat3", nil, "std1", 0)

	// GetPrimitiveMetrics
	metrics := db.GetPrimitiveMetrics(1000)
	if metrics == nil {
		t.Error("expected non-nil metrics")
	}

	// GetTopQueries
	queries := db.GetTopQueries(5)
	if len(queries) > 0 {
		// Just ensuring it doesn't panic
		t.Logf("queries: %v", queries)
	}

	// BatchDelete
	err = db.BatchDelete(context.Background(), []string{"k1", "k2"})
	if err != nil {
		t.Errorf("expected no error from batch delete, got %v", err)
	}

	// VacuumProjects
	_, err = db.VacuumProjects(context.Background(), true)
	if err != nil {
		t.Errorf("expected no error from vacuum projects, got %v", err)
	}

	// GetByAttributes
	entries, err := db.GetByAttributes(context.Background(), DomainStandards, &AttributeQuery{})
	if err != nil {
		t.Errorf("expected no error from GetByAttributes, got %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries")
	}

	// GetTTLHorizon
	horizon, _, _ := db.GetTTLHorizon(context.Background(), 10)
	if horizon != 0 {
		t.Log("horizon: ", horizon)
	}

	// DeleteBatch
	err = db.DeleteBatch(context.Background(), []string{"k1", "k2"})
	if err != nil {
		t.Errorf("DeleteBatch expected no error, got %v", err)
	}

	// DeleteDomain
	_, err = db.DeleteDomain(context.Background(), DomainSessions)
	if err != nil {
		t.Errorf("DeleteDomain expected no error, got %v", err)
	}

	// DeleteProjects
	_, err = db.DeleteProjects(context.Background(), "", "")
	if err != nil {
		t.Errorf("DeleteProjects expected no error, got %v", err)
	}

	// DeleteStandards
	_, err = db.DeleteStandards(context.Background(), "", "")
	if err != nil {
		t.Errorf("DeleteStandards expected no error, got %v", err)
	}

	// SearchSessions
	res, err := db.SearchSessions(context.Background(), "query", "", "", "", "", "", 10)
	if err != nil {
		t.Errorf("SearchSessions expected no error, got %v", err)
	}
	if res == nil {
		t.Errorf("expected non-nil res")
	}

	// GetDBSize
	size, _ := db.GetDBSize()
	if size < 0 {
		t.Errorf("expected size >= 0")
	}

	// Clear
	_ = db.Clear(context.Background())

	// ExportJSONL
	tempDir := t.TempDir()
	exportPath := tempDir + "/export.jsonl"
	_, _ = db.ExportJSONL(context.Background(), exportPath, "", nil)

	// ListSessions
	_, _ = db.ListSessions(context.Background(), DomainSessions, "", "", "", "", 10)

	// PurgeDomain
	_, _ = db.PurgeDomain(context.Background(), DomainSessions)

	// Delete
	_ = db.Delete(context.Background(), "fake-key")

	// GetBatch
	_, _, _ = db.GetBatch(context.Background(), []string{"k1", "k2"})

	// FindSessionBySuffix
	_, _ = db.FindSessionBySuffix(context.Background(), DomainSessions, "suffix")

	// SearchSessions
	_, _ = db.SearchSessions(context.Background(), DomainSessions, "query", "", "", "", "", 10)

	// GetByAttributes
	_, _ = db.GetByAttributes(context.Background(), DomainMemories, &AttributeQuery{})

	// GetTTLHorizon
	_, _, _ = db.GetTTLHorizon(context.Background(), 30)

	// flushNamespaces
	db.flushNamespaces(t.TempDir())

	// SyncSearchIndex
	_ = db.SyncSearchIndex(context.Background())

	// Search
	_, _ = db.Search(context.Background(), "query", "tag", 10)

	// StartConfigWatcher
	db.StartConfigWatcher(t.TempDir(), func() {})
}

func TestCloseExportFile(t *testing.T) {
	closeExportFile(nil, "")
}

func TestBadgerInternalFunctions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m, err := NewMemoryStore(ctx, t.TempDir(), "", 10, config.BatchConfig{})
	if err != nil {
		t.Fatalf("Failed to init memory: %v", err)
	}
	defer m.Close()

	// test SyncSearchIndex
	m.SyncSearchIndex(ctx)

	// Stop the GC goroutine NewMemoryStore started. Do not swap s.ctx while
	// runGC is reading it — that is a data race.
	cancel()

	// test DeleteStandards
	_, _ = m.DeleteStandards(ctx, "test-cat", "test-pkg")
	_, _ = m.DeleteStandards(ctx, "", "")

	// test SearchSessions
	_, _ = m.SearchSessions(ctx, "test", "foo", "", "", "", "", 10)

	// test SearchDomain
	_, _ = m.SearchDomain(ctx, SearchDomainQuery{Domain: "test-domain", Query: "test"})

	// test matchesExportFilters
	_ = matchesExportFilters(&Record{Category: "foo"}, "foo", []string{"bar"})
	_ = matchesExportFilters(&Record{Category: "baz"}, "", []string{"foo"})

	// test indexSaveRecordToSearch

	// Open bad badger path to trigger openBadgerWithRetry errors
	opts := badger.DefaultOptions("/dev/null/invalid")
	_, _ = openBadgerWithRetry(opts, 1)
}

func TestRecordMatchesDomainSearch(t *testing.T) {
	rec := &Record{
		Category: "test",
		Tags:     []string{"type:struct", "implements:foo", "receiver:bar", "domain:baz", "tag1", "tag2"},
	}

	if recordMatchesDomainSearch(SearchDomainQuery{SymbolType: "nomatch"}, rec) {
		t.Fatal("should not match")
	}
	if recordMatchesDomainSearch(SearchDomainQuery{Interface: "nomatch"}, rec) {
		t.Fatal("should not match")
	}
	if recordMatchesDomainSearch(SearchDomainQuery{Receiver: "nomatch"}, rec) {
		t.Fatal("should not match")
	}
	if recordMatchesDomainSearch(SearchDomainQuery{Domain: "nomatch"}, rec) {
		t.Fatal("should not match")
	}
	if recordMatchesDomainSearch(SearchDomainQuery{Tags: []string{"tag1", "tag3"}, TagMatchMode: "all"}, rec) {
		t.Fatal("should not match")
	}
	if !recordMatchesDomainSearch(SearchDomainQuery{Tags: []string{"tag1", "tag3"}, TagMatchMode: "any"}, rec) {
		t.Fatal("should match any")
	}

	// Category no longer gates domain search. The SysDrift exclusion was removed
	// with the harvest subsystem (0005-MADR); an unfiltered query matches any
	// record regardless of category.
	if !recordMatchesDomainSearch(SearchDomainQuery{}, &Record{Category: "SysDrift"}) {
		t.Fatal("unfiltered query should match regardless of category")
	}
	if !recordMatchesDomainSearch(SearchDomainQuery{}, &Record{Category: "AnythingElse"}) {
		t.Fatal("unfiltered query should match an arbitrary category")
	}
}

func TestSyncSearchIndexAndSearchDomain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := NewMemoryStore(ctx, t.TempDir(), "", 100, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed to create memory store: %v", err)
	}
	defer db.Close()

	// Enable search engine for test
	bleveEngine, err := search.InitStorage(filepath.Join(t.TempDir(), "test.bleve"))
	if err == nil {
		db.mu.Lock()
		db.search = bleveEngine
		db.mu.Unlock()
	}

	_, _ = db.Save(ctx, "std1", "std1", "standard content 1", "test-cat", nil, DomainStandards, 0.9)
	_, _ = db.Save(ctx, "pkg:mypkg:file1", "file1", "package content 1", "test-cat", nil, DomainStandards, 0.9)

	err = db.SyncSearchIndex(ctx)
	if err != nil {
		t.Fatalf("failed to sync search index: %v", err)
	}

	// Test SearchDomain
	q := SearchDomainQuery{
		Query:        "standard",
		TargetDomain: DomainStandards,
		Limit:        10,
	}
	seq, err := db.SearchDomain(ctx, q)
	if err != nil {
		t.Fatalf("SearchDomain failed: %v", err)
	}
	count := 0
	for range seq {
		count++
	}
	if count == 0 {
		t.Logf("SearchDomain yielded no results, expected some")
	}

	// Delete standards
	_, _ = db.DeleteStandards(ctx, "test-cat", "")
}

func TestDeleteProjects(t *testing.T) {
	ctx := context.Background()
	store, err := NewMemoryStore(ctx, t.TempDir(), "", 100, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Add a project
	_, err = store.Save(ctx, "proj1-title", "proj1", "project one", "HarvestedCode", nil, DomainProjects, 1.0)
	if err != nil {
		t.Fatalf("failed to save: %v", err)
	}

	// Delete it
	deleted, err := store.DeleteProjects(ctx, "HarvestedCode", "")
	if err != nil {
		t.Errorf("DeleteProjects failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected to delete 1, got %d", deleted)
	}

	// Delete again, should be 0
	deleted, err = store.DeleteProjects(ctx, "HarvestedCode", "")
	if err != nil {
		t.Errorf("DeleteProjects failed: %v", err)
	}
	if deleted != 0 {
		t.Errorf("expected to delete 0, got %d", deleted)
	}

	// An unknown category is no longer rejected up front — the harvest-category
	// allowlist was removed in 0005-MADR. It is now a domain-gated no-op.
	unknownDeleted, err := store.DeleteProjects(ctx, "invalid", "")
	if err != nil {
		t.Errorf("expected no error for unknown category, got %v", err)
	}
	if unknownDeleted != 0 {
		t.Errorf("expected 0 deletions for unknown projects category, got %d", unknownDeleted)
	}

	// With pkg inside category loop
	_, _ = store.Save(ctx, "proj-pkg-title", "pkg:pkg1:proj", "project with pkg", "HarvestedCode", nil, DomainProjects, 1.0)
	// Add a non-pkg1 project to ensure we skip it
	_, _ = store.Save(ctx, "proj-pkg-title2", "pkg:pkg2:proj", "project with pkg2", "HarvestedCode", nil, DomainProjects, 1.0)
	deleted, err = store.DeleteProjects(ctx, "HarvestedCode", "pkg1")
	if err != nil {
		t.Errorf("DeleteProjects with pkg failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("Expected 1, got %d", deleted)
	}

	// Delete by pkg only (empty category)
	deleted, err = store.DeleteProjects(ctx, "", "pkg2")
	if err != nil {
		t.Errorf("DeleteProjects by pkg only failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("Expected 1, got %d", deleted)
	}

	// Global sweep
	_, _ = store.Save(ctx, "proj2-title", "proj2", "project two", "HarvestedCode", nil, DomainProjects, 1.0)
	deleted, err = store.DeleteProjects(ctx, "", "")
	if err != nil {
		t.Errorf("DeleteProjects global sweep failed: %v", err)
	}
	if deleted < 1 {
		t.Errorf("expected global sweep to delete >= 1, got %d", deleted)
	}
}

func TestDeleteStandards(t *testing.T) {
	ctx := context.Background()
	store, err := NewMemoryStore(ctx, t.TempDir(), "", 100, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Add a standard
	_, err = store.Save(ctx, "std1-title", "std1", "standard one", "HarvestedCode", nil, DomainStandards, 1.0)
	if err != nil {
		t.Fatalf("failed to save: %v", err)
	}

	// Delete it
	deleted, err := store.DeleteStandards(ctx, "HarvestedCode", "")
	if err != nil {
		t.Errorf("DeleteStandards failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected to delete 1, got %d", deleted)
	}

	// Delete again, should be 0
	deleted, err = store.DeleteStandards(ctx, "HarvestedCode", "")
	if err != nil {
		t.Errorf("DeleteStandards failed: %v", err)
	}
	if deleted != 0 {
		t.Errorf("expected to delete 0, got %d", deleted)
	}

	// An unknown category is no longer rejected up front — the harvest-category
	// allowlist was removed in 0005-MADR. It is now a domain-gated no-op.
	unknownDeleted, err := store.DeleteStandards(ctx, "invalid", "")
	if err != nil {
		t.Errorf("expected no error for unknown category, got %v", err)
	}
	if unknownDeleted != 0 {
		t.Errorf("expected 0 deletions for unknown standards category, got %d", unknownDeleted)
	}

	// With pkg inside category loop
	_, _ = store.Save(ctx, "std-pkg-title", "pkg:pkg1:std", "standard with pkg", "HarvestedCode", nil, DomainStandards, 1.0)
	// Add a non-pkg1 standard to ensure we skip it
	_, _ = store.Save(ctx, "std-pkg-title2", "pkg:pkg2:std", "standard with pkg2", "HarvestedCode", nil, DomainStandards, 1.0)
	deleted, err = store.DeleteStandards(ctx, "HarvestedCode", "pkg1")
	if err != nil {
		t.Errorf("DeleteStandards with pkg failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("Expected 1, got %d", deleted)
	}

	// Delete by pkg only (empty category)
	deleted, err = store.DeleteStandards(ctx, "", "pkg2")
	if err != nil {
		t.Errorf("DeleteStandards by pkg only failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("Expected 1, got %d", deleted)
	}

	// Global sweep
	_, _ = store.Save(ctx, "std2-title", "std2", "standard two", "HarvestedCode", nil, DomainStandards, 1.0)
	deleted, err = store.DeleteStandards(ctx, "", "")
	if err != nil {
		t.Errorf("DeleteStandards global sweep failed: %v", err)
	}
	if deleted < 1 {
		t.Errorf("expected global sweep to delete >= 1, got %d", deleted)
	}
}

func TestBatchDelete(t *testing.T) {
	ctx := context.Background()
	store, err := NewMemoryStore(ctx, t.TempDir(), "", 100, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	keys := []string{"batch1", "batch2"}
	for _, k := range keys {
		_, err = store.Save(ctx, k+"-title", k, "content", "category", nil, DomainMemories, 1.0)
		if err != nil {
			t.Fatalf("failed to save %s: %v", k, err)
		}
	}

	err = store.BatchDelete(ctx, keys)
	if err != nil {
		t.Fatalf("BatchDelete failed: %v", err)
	}

	// Verify they are gone
	for _, k := range keys {
		rec, _ := store.Get(ctx, k)
		if rec != nil {
			t.Errorf("expected %s to be deleted", k)
		}
	}
}

func TestFindSessionBySuffix(t *testing.T) {
	ctx := context.Background()
	store, err := NewMemoryStore(ctx, t.TempDir(), "", 100, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	_, err = store.Save(ctx, "session-title", "session_12345", "content", "category", nil, DomainSessions, 1.0)
	if err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	res, err := store.FindSessionBySuffix(ctx, DomainSessions, "345")
	if err != nil {
		t.Errorf("FindSessionBySuffix failed: %v", err)
	}
	if res == nil || res.Key != "session_12345" {
		t.Errorf("expected session_12345, got res %v", res)
	}

	res, err = store.FindSessionBySuffix(ctx, DomainSessions, "999")
	if err != nil {
		t.Errorf("expected no error for not found, got %v", err)
	}
	if res != nil {
		t.Errorf("expected res to be nil for not found, got %v", res)
	}
}

func TestIndexSaveRecordToSearch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tmpDir := t.TempDir()
	db, err := NewMemoryStore(ctx, tmpDir, "", 100, config.New("test").BatchSettings())
	require.NoError(t, err)
	defer db.Close()

	engine, err := search.InitStorage(t.TempDir())
	require.NoError(t, err)
	_ = db.SetSearchEngine(ctx, engine)
	require.NotNil(t, db.search)

	// test "pkg:somepkg:somesym"
	indexSaveRecordToSearch(db.search, "pkg:mypkg:mysym", "title", "content", "cat", []string{"tag1"}, DomainProjects)
	// test "pkg:somepkg"
	indexSaveRecordToSearch(db.search, "pkg:mypkg", "title", "content", "cat", []string{"tag1"}, DomainProjects)

	// test without pkg prefix
	indexSaveRecordToSearch(db.search, "normal-key", "title", "content", "cat", []string{"tag1"}, DomainProjects)
}

func TestPruneDomain(t *testing.T) {
	ctx := context.Background()
	store, err := NewMemoryStore(ctx, t.TempDir(), "", 100, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	engine, _ := search.InitStorage(t.TempDir())
	_ = store.SetSearchEngine(ctx, engine)

	_, _ = store.Save(ctx, "std1", "std1", "content", "cat", nil, DomainStandards, 1.0)

	count, err := store.PruneDomain(ctx, DomainStandards, -1) // force prune
	if err != nil {
		t.Errorf("PruneDomain failed: %v", err)
	}
	if count < 0 {
		t.Errorf("expected non-negative count")
	}

	count, err = store.PruneDomain(ctx, "invalid_domain", -1)
	if err != nil {
		t.Errorf("expected no error for invalid domain, got: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 for invalid domain, got: %d", count)
	}
}

func TestPurgeDomain(t *testing.T) {
	ctx := context.Background()
	store, err := NewMemoryStore(ctx, t.TempDir(), "", 100, config.New("test").BatchSettings())
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	engine, _ := search.InitStorage(t.TempDir())
	_ = store.SetSearchEngine(ctx, engine)

	_, _ = store.Save(ctx, "proj1", "proj1", "content", "cat", nil, DomainProjects, 1.0)

	count, err := store.PurgeDomain(ctx, DomainProjects)
	if err != nil {
		t.Errorf("PurgeDomain failed: %v", err)
	}
	if count < 0 {
		t.Errorf("expected non-negative count")
	}

	count, err = store.PurgeDomain(ctx, "invalid_domain")
	if err != nil {
		t.Errorf("expected no error for invalid domain, got: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 for invalid domain, got: %d", count)
	}
}
