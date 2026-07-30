package search

import (
	"context"
	"fmt"
	"testing"
)

// helper to create a test engine with seeded docs.
func newTestEngine(t *testing.T, docs map[string]*Document) *BleveEngine {
	t.Helper()
	engine, err := InitStorage(t.TempDir())
	if err != nil {
		t.Fatalf("InitStorage() failed: %v", err)
	}
	if len(docs) > 0 {
		if err := engine.Rebuild(context.Background(), docs); err != nil {
			t.Fatalf("Rebuild() failed: %v", err)
		}
	}
	return engine
}

func TestInitStorage(t *testing.T) {
	engine, err := InitStorage(t.TempDir())
	if err != nil {
		t.Fatalf("InitStorage() error: %v", err)
	}
	defer engine.Close()

	count, err := engine.DocCount()
	if err != nil {
		t.Fatalf("DocCount() error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 docs, got %d", count)
	}
}

func TestIndex_And_Search(t *testing.T) {
	engine := newTestEngine(t, nil)
	defer engine.Close()

	doc := &Document{
		Content:  "Kubernetes deployment strategy using blue-green pattern",
		Category: "devops",
		Tags:     []string{"kubernetes", "deployment"},
	}

	if err := engine.Index("k8s-deploy", doc); err != nil {
		t.Fatalf("Index() error: %v", err)
	}

	// Content search via Bleve.
	hits, err := engine.Search(context.Background(), "kubernetes deployment", []string{"k8s-deploy"}, 10)
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected at least 1 hit, got 0")
	}

	found := false
	for _, h := range hits {
		if h.ID == "k8s-deploy" {
			found = true
			if h.Score <= 0 {
				t.Errorf("expected positive score, got %f", h.Score)
			}
		}
	}
	if !found {
		t.Error("expected to find k8s-deploy in results")
	}
}

func TestIndexBatch_BulkWrite(t *testing.T) {
	engine := newTestEngine(t, nil)
	defer engine.Close()

	docs := make(map[string]*Document, 50)
	for i := range 50 {
		docs[fmt.Sprintf("doc-%d", i)] = &Document{
			Content:  fmt.Sprintf("Document number %d about Go performance", i),
			Category: "testing",
		}
	}

	if err := engine.IndexBatch(docs); err != nil {
		t.Fatalf("IndexBatch() error: %v", err)
	}

	count, err := engine.DocCount()
	if err != nil {
		t.Fatalf("DocCount() error: %v", err)
	}
	if count != 50 {
		t.Errorf("expected 50 docs, got %d", count)
	}

	// Verify searchable.
	hits, err := engine.Search(context.Background(), "Go performance", nil, 10)
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(hits) == 0 {
		t.Error("expected hits for 'Go performance', got 0")
	}
}

func TestDelete_RemovesFromIndex(t *testing.T) {
	engine := newTestEngine(t, map[string]*Document{
		"to-delete": {Content: "This record will be deleted", Category: "test"},
		"to-keep":   {Content: "This record stays", Category: "test"},
	})
	defer engine.Close()

	if err := engine.Delete("to-delete"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	count, _ := engine.DocCount()
	if count != 1 {
		t.Errorf("expected 1 doc after delete, got %d", count)
	}

	hits, _ := engine.Search(context.Background(), "deleted", []string{"to-keep"}, 10)
	for _, h := range hits {
		if h.ID == "to-delete" {
			t.Error("deleted document should not appear in search results")
		}
	}
}

func TestDeleteBatch_BulkRemoval(t *testing.T) {
	docs := make(map[string]*Document, 10)
	for i := range 10 {
		docs[fmt.Sprintf("batch-%d", i)] = &Document{Content: fmt.Sprintf("batch content %d", i)}
	}

	engine := newTestEngine(t, docs)
	defer engine.Close()

	// Delete first 5.
	ids := make([]string, 5)
	for i := range 5 {
		ids[i] = fmt.Sprintf("batch-%d", i)
	}

	if err := engine.DeleteBatch(ids); err != nil {
		t.Fatalf("DeleteBatch() error: %v", err)
	}

	count, _ := engine.DocCount()
	if count != 5 {
		t.Errorf("expected 5 docs after batch delete, got %d", count)
	}
}

func TestRebuild_ColdStart(t *testing.T) {
	engine := newTestEngine(t, nil)
	defer engine.Close()

	// Index some docs first.
	_ = engine.Index("stale", &Document{Content: "stale data"})

	// Rebuild with fresh data.
	freshDocs := map[string]*Document{
		"fresh-1": {Content: "Fresh document one"},
		"fresh-2": {Content: "Fresh document two"},
	}

	if err := engine.Rebuild(context.Background(), freshDocs); err != nil {
		t.Fatalf("Rebuild() error: %v", err)
	}

	count, _ := engine.DocCount()
	if count != 2 {
		t.Errorf("expected 2 docs after rebuild, got %d", count)
	}

	// Stale doc should be gone.
	hits, _ := engine.Search(context.Background(), "stale", []string{"stale"}, 10)
	for _, h := range hits {
		if h.ID == "stale" && h.Source == "bleve" {
			t.Error("stale document should not appear in Bleve results after rebuild")
		}
	}
}

func TestSearch_DualEngine(t *testing.T) {
	engine := newTestEngine(t, map[string]*Document{
		"prometheus_alerting_rules": {Content: "Configure alertmanager receivers for PagerDuty", Category: "monitoring"},
		"badger_gc_config":          {Content: "BadgerDB garbage collection tuning parameters", Category: "database"},
	})
	defer engine.Close()

	keys := []string{"prometheus_alerting_rules", "badger_gc_config"}

	// "prometheus" should match "prometheus_alerting_rules" via fuzzy
	// (direct subsequence in the key), and may also match via Bleve if
	// the content contains the term.
	hits, err := engine.Search(context.Background(), "prometheus", keys, 10)
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}

	foundFuzzy := false
	for _, h := range hits {
		if h.ID == "prometheus_alerting_rules" && (h.Source == "fuzzy" || h.Source == "both") {
			foundFuzzy = true
		}
	}
	if !foundFuzzy {
		t.Error("expected fuzzy engine to find 'prometheus_alerting_rules' for query 'prometheus'")
	}

	// "alertmanager" should match via Bleve (content search).
	hits2, err := engine.Search(context.Background(), "alertmanager", keys, 10)
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}

	foundBleve := false
	for _, h := range hits2 {
		if h.ID == "prometheus_alerting_rules" && (h.Source == "bleve" || h.Source == "both") {
			foundBleve = true
		}
	}
	if !foundBleve {
		t.Error("expected Bleve engine to find 'prometheus_alerting_rules' for query 'alertmanager'")
	}
}

func TestSearch_ScoreNormalization(t *testing.T) {
	engine := newTestEngine(t, map[string]*Document{
		"doc-a": {Content: "Alpha bravo charlie delta"},
		"doc-b": {Content: "Alpha bravo"},
	})
	defer engine.Close()

	hits, _ := engine.Search(context.Background(), "alpha bravo", []string{"doc-a", "doc-b"}, 10)

	for _, h := range hits {
		if h.Score < 0 || h.Score > 1.0 {
			t.Errorf("score %f for %q is out of [0,1] range", h.Score, h.ID)
		}
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	engine := newTestEngine(t, map[string]*Document{
		"doc": {Content: "some content"},
	})
	defer engine.Close()

	hits, err := engine.Search(context.Background(), "", nil, 10)
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected 0 hits for empty query, got %d", len(hits))
	}
}

func TestDocCount(t *testing.T) {
	docs := map[string]*Document{
		"a": {Content: "one"},
		"b": {Content: "two"},
		"c": {Content: "three"},
	}
	engine := newTestEngine(t, docs)
	defer engine.Close()

	count, err := engine.DocCount()
	if err != nil {
		t.Fatalf("DocCount() error: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3, got %d", count)
	}
}

func TestSearchScoped(t *testing.T) {
	engine := newTestEngine(t, map[string]*Document{
		"pkg:domainA:mod1:func": {Content: "test authentication", Category: "code"},
		"pkg:domainA:mod2:func": {Content: "test authorization", Category: "code"},
		"pkg:domainB:mod1:func": {Content: "test database", Category: "other"},
	})
	defer engine.Close()

	// Should only find the ones in category "code"
	hits, err := engine.SearchScoped(context.Background(), "test", []string{"code"}, nil, 10)
	if err != nil {
		t.Fatalf("SearchScoped() error: %v", err)
	}
	if len(hits) != 2 {
		t.Errorf("expected 2 hits, got %d", len(hits))
	}
	for _, h := range hits {
		if h.ID == "pkg:domainB:mod1:func" {
			t.Errorf("expected domainB to be filtered out")
		}
	}
}

func TestSearchScopedSeq(t *testing.T) {
	engine := newTestEngine(t, map[string]*Document{
		"seq:sess1:mem1": {Content: "what is the best db?", Category: "session"},
		"seq:sess1:mem2": {Content: "badgerdb is fast", Category: "session"},
		"seq:sess2:mem1": {Content: "badgerdb config", Category: "session"},
	})
	defer engine.Close()

	// Find badgerdb within category "session"
	seq := engine.SearchScopedSeq(context.Background(), "badgerdb", []string{"session"}, nil, 10)

	count := 0
	foundFast := false
	for id, score := range seq {
		count++
		if score <= 0 {
			t.Errorf("expected positive score, got %f for %s", score, id)
		}
		if id == "seq:sess1:mem2" {
			foundFast = true
		}
	}

	if count != 2 {
		t.Errorf("expected 2 hits, got %d", count)
	}
	if !foundFast {
		t.Errorf("expected hit seq:sess1:mem2")
	}
}

func TestHas(t *testing.T) {
	engine := newTestEngine(t, map[string]*Document{
		"existing": {Content: "test"},
	})
	defer engine.Close()

	has, _ := engine.Has("existing")
	if !has {
		t.Error("expected Has to return true for existing doc")
	}
	has, _ = engine.Has("missing")
	if has {
		t.Error("expected Has to return false for missing doc")
	}
}

func TestMergeHits_Deduplication(t *testing.T) {
	bleveHits := []SearchHit{
		{ID: "shared", Score: 0.8, Source: "bleve"},
		{ID: "bleve-only", Score: 0.6, Source: "bleve"},
	}
	fuzzyHits := []SearchHit{
		{ID: "shared", Score: 0.5, Source: "fuzzy"},
		{ID: "fuzzy-only", Score: 0.9, Source: "fuzzy"},
	}

	merged := mergeHits(bleveHits, fuzzyHits)

	if len(merged) != 3 {
		t.Fatalf("expected 3 merged hits, got %d", len(merged))
	}

	for _, h := range merged {
		if h.ID == "shared" {
			if h.Source != "both" {
				t.Errorf("shared hit source should be 'both', got %q", h.Source)
			}
			if h.Score != 0.8 {
				t.Errorf("shared hit should keep max score 0.8, got %f", h.Score)
			}
		}
	}
}

func BenchmarkSearch_1000Docs(b *testing.B) {
	docs := make(map[string]*Document, 1000)
	for i := range 1000 {
		docs[fmt.Sprintf("doc-%d", i)] = &Document{
			Content:  fmt.Sprintf("Document %d about Go systems engineering and performance optimization", i),
			Category: "benchmark",
			Tags:     []string{"go", "performance"},
		}
	}

	engine, err := InitStorage(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	defer engine.Close()

	if err := engine.Rebuild(context.Background(), docs); err != nil {
		b.Fatal(err)
	}

	keys := make([]string, 0, 1000)
	for k := range docs {
		keys = append(keys, k)
	}

	ctx := context.Background()
	b.ResetTimer()
	for range b.N {
		_, _ = engine.Search(ctx, "Go performance optimization", keys, 20)
	}
}
