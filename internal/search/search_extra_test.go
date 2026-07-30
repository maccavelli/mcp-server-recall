package search

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBestEffortRename(t *testing.T) {
	d := t.TempDir()
	p1 := filepath.Join(d, "a")
	p2 := filepath.Join(d, "b")
	os.WriteFile(p1, []byte("x"), 0644)
	bestEffortRename(p1, p2)

	bestEffortRename("does_not_exist", p2)
}

func TestIsRebuilding(t *testing.T) {
	idx, err := InitStorage(t.TempDir())
	if err == nil {
		defer idx.Close()
		_ = idx.IsRebuilding()
	}
}

func TestSearchScopedAndSeq(t *testing.T) {
	tmpDir := t.TempDir()
	eng, err := InitStorage(tmpDir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer eng.Close()

	// 1. empty query
	hits, err := eng.SearchScoped(context.Background(), "", nil, nil, 10)
	if err != nil {
		t.Errorf("expected no error for empty query, got %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected 0 hits for empty query")
	}

	seq := eng.SearchScopedSeq(context.Background(), "", nil, nil, 10)
	for range seq {
		t.Errorf("should not yield")
	}

	// 2. index some documents
	docs := map[string]*Document{
		"doc1": {Content: "this is a test document about cats", Category: "animals", Tags: []string{"feline"}},
		"doc2": {Content: "this is another document about dogs", Category: "animals", Tags: []string{"canine"}},
	}
	err = eng.IndexBatch(docs)
	if err != nil {
		t.Fatalf("failed to index: %v", err)
	}

	// 3. query with category and tags
	hits, err = eng.SearchScoped(context.Background(), "document", []string{"animals"}, []string{"feline"}, 10)
	if err != nil {
		t.Fatalf("search scoped failed: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "doc1" {
		t.Errorf("expected 1 hit for doc1, got %v", hits)
	}

	// 4. seq query with category and tags
	seq = eng.SearchScopedSeq(context.Background(), "document", []string{"animals"}, []string{"canine"}, 10)
	count := 0
	for id, score := range seq {
		if id != "doc2" {
			t.Errorf("expected doc2, got %s", id)
		}
		if score <= 0 {
			t.Errorf("expected positive score")
		}
		count++
	}
	if count != 1 {
		t.Errorf("expected 1 hit, got %d", count)
	}

	// 5. Test Has, Index, Delete
	// 5. Test Has, Index, Delete
	// Has on non-existent
	has, err := eng.Has("doc_new")
	if err != nil || has {
		t.Errorf("expected not to have doc_new, got %v, err %v", has, err)
	}

	// Index new document
	err = eng.Index("doc_new", &Document{Content: "single doc", Category: "animals"})
	if err != nil {
		t.Fatalf("failed to index single doc: %v", err)
	}

	// Has on existent
	has, err = eng.Has("doc_new")
	if err != nil || !has {
		t.Errorf("expected to have doc_new, got %v, err %v", has, err)
	}

	// Delete document
	err = eng.Delete("doc_new")
	if err != nil {
		t.Fatalf("failed to delete doc: %v", err)
	}

	// Has after delete
	has, err = eng.Has("doc_new")
	if err != nil || has {
		t.Errorf("expected not to have doc_new after delete, got %v, err %v", has, err)
	}
}
