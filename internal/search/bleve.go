// Package search provides functionality for the search subsystem.
package search

import (
	"context"
	"fmt"
	"iter"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/sahilm/fuzzy"

	_ "github.com/blevesearch/bleve/v2/analysis/analyzer/custom"  // register custom analyzer support
	_ "github.com/blevesearch/bleve/v2/analysis/token/camelcase"  // register camelCase token filter
	_ "github.com/blevesearch/bleve/v2/analysis/token/lowercase"  // register lowercase token filter
	_ "github.com/blevesearch/bleve/v2/analysis/tokenizer/regexp" // register regexp tokenizer
)

func bestEffortRemoveAll(path string) {
	if err := os.RemoveAll(path); err != nil {
		slog.Debug("failed to remove search index path", "path", path, "error", err)
	}
}

func bestEffortRename(oldpath, newpath string) {
	if err := os.Rename(oldpath, newpath); err != nil {
		slog.Debug("failed to rename search index path", "from", oldpath, "to", newpath, "error", err)
	}
}

// BleveEngine implements SearchEngine using an in-memory Bleve index for
// full-text content search and sahilm/fuzzy for key discovery.
type BleveEngine struct {
	index            bleve.Index
	path             string
	mu               sync.RWMutex
	closeOnce        sync.Once
	rebuildBatchSize int
	rebuilding       atomic.Bool // Suppresses phantom drift alerts during active rebuilds
}

// IsRebuilding returns true if the engine is currently performing a rebuild.
// Used by the audit worker to suppress phantom drift alerts during rebuild windows.
func (e *BleveEngine) IsRebuilding() bool {
	return e.rebuilding.Load()
}

// bleveDoc is the struct indexed by Bleve. Fields are analyzed and indexed
// but NOT stored (Store=false in the mapping), keeping memory minimal.
type bleveDoc struct {
	Title      string   `json:"title"`
	SymbolName string   `json:"symbolname"`
	Content    string   `json:"content"`
	Category   string   `json:"category"`
	Tags       []string `json:"tags"`
	SourcePath string   `json:"sourcepath"`
	SourceHash string   `json:"sourcehash"`
}

// InitStorage creates or opens a persistent Bleve index utilizing Scorch.
func InitStorage(path string) (*BleveEngine, error) {
	var idx bleve.Index
	var err error

	// Build scorch kvconfig from fixed footprints
	// 🛡️ TUNING (20k Bounds): Leverages 2048MB GOMEMLIMIT for high-performance indexing
	kvconfig := map[string]any{
		"unsafe_batch":       false,
		"numSnapshotsToKeep": 1,
		"scorchPersisterOptions": map[string]any{
			"PersisterNapTimeMSec":      0,
			"PersisterNapUnderNumFiles": 1000,
			// 🛡️ CPU CAP: Native 2-Core Topology Match
			"NumPersisterWorkers":           2,
			"MaxSizeInMemoryMergePerWorker": 256000000, // 256MB merge buffer cap
		},
		"scorchMergePlanOptions": map[string]any{
			"MaxSegmentsPerTier":   10,
			"MaxSegmentSize":       32000000, // 32MB segments reduce fragmentation
			"TierGrowth":           4.0,
			"SegmentsPerMergeTask": 10,
			"FloorSegmentSize":     2000,
		},
	}

	if statInfo, statErr := os.Stat(filepath.Join(path, "index_meta.json")); statErr == nil && !statInfo.IsDir() {
		idx, err = bleve.Open(path)
		if err != nil {
			return nil, fmt.Errorf("failed to open existing bleve index (is it locked?): %w", err)
		}
		slog.Info("BleveEngine initialized (Opened existing disk index)", "path", path)
	} else {
		m, err := buildMapping()
		if err != nil {
			return nil, fmt.Errorf("failed to build mapping: %w", err)
		}
		idx, err = bleve.NewUsing(path, m, "scorch", "scorch", kvconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create new bleve index on disk: %w", err)
		}
		slog.Info("BleveEngine initialized (Created new disk index)", "path", path)
	}

	return &BleveEngine{
		index: idx,
		path:  path,
		// 🛡️ RAM BOUND: 2000-doc batches for efficient cold-start backfill within 2048MB budget
		rebuildBatchSize: 2000,
	}, nil
}

// buildMapping creates the index mapping for recall documents.
// All fields are indexed for search but NOT stored (ID-only footprint).
func buildMapping() (*mapping.IndexMappingImpl, error) {
	indexMapping := bleve.NewIndexMapping()

	err := indexMapping.AddCustomTokenizer("tech_tokenizer", map[string]any{
		"type":   "regexp",
		"regexp": `[a-zA-Z0-9_\.]+`,
	})
	if err != nil {
		return nil, err
	}

	err = indexMapping.AddCustomAnalyzer("tech_analyzer", map[string]any{ //nolint:gosec // analyzer name, not a credential
		"type":          "custom",
		"tokenizer":     "tech_tokenizer",
		"token_filters": []string{"camelCase", "to_lower"},
	})
	if err != nil {
		return nil, err
	}

	techField := bleve.NewTextFieldMapping()
	techField.Store = false
	techField.Index = true
	techField.IncludeTermVectors = false
	techField.Analyzer = "tech_analyzer"

	keywordField := bleve.NewKeywordFieldMapping()
	keywordField.Store = false
	keywordField.Index = true

	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("title", techField)
	docMapping.AddFieldMappingsAt("symbolname", techField)
	docMapping.AddFieldMappingsAt("content", techField)
	docMapping.AddFieldMappingsAt("category", keywordField)
	docMapping.AddFieldMappingsAt("tags", keywordField)

	sourceField := bleve.NewKeywordFieldMapping()
	sourceField.Store = false
	sourceField.Index = true
	docMapping.AddFieldMappingsAt("sourcepath", sourceField)
	docMapping.AddFieldMappingsAt("sourcehash", sourceField)

	indexMapping.DefaultMapping = docMapping
	indexMapping.DefaultMapping.Dynamic = false
	indexMapping.DefaultAnalyzer = "tech_analyzer"

	return indexMapping, nil
}

// toBleveDoc converts a Document to the Bleve-indexable representation.
func toBleveDoc(doc *Document) bleveDoc {
	return bleveDoc{
		Title:      doc.Title,
		SymbolName: doc.SymbolName,
		Content:    doc.Content,
		Category:   doc.Category,
		Tags:       doc.Tags,
		SourcePath: doc.SourcePath,
		SourceHash: doc.SourceHash,
	}
}

// Rebuild re-indexes all documents from the source of truth using a batch
// write for efficiency. This is the cold-start path.
func (e *BleveEngine) Rebuild(ctx context.Context, docs map[string]*Document) error {
	e.rebuilding.Store(true)
	defer e.rebuilding.Store(false)

	e.mu.Lock()
	defer e.mu.Unlock()

	// Close the old index before rebuilding.
	if e.index != nil {
		if err := e.index.Close(); err != nil {
			slog.Warn("BleveEngine: failed to close old index during rebuild", "error", err)
		}
		e.index = nil
	}

	// 🛡️ Crash-safe rebuild: build into a staging directory, then atomic swap.
	tmpPath := e.path + ".rebuilding"
	if err := os.RemoveAll(tmpPath); err != nil {
		slog.Warn("BleveEngine: failed to remove stale staging directory", "error", err)
	}

	m, err := buildMapping()
	if err != nil {
		return fmt.Errorf("failed to build mapping during rebuild: %w", err)
	}

	kvconfig := map[string]any{
		"unsafe_batch":       false,
		"numSnapshotsToKeep": 1,
		"scorchPersisterOptions": map[string]any{
			"PersisterNapTimeMSec":          0,
			"PersisterNapUnderNumFiles":     1000,
			"NumPersisterWorkers":           2,
			"MaxSizeInMemoryMergePerWorker": 256000000, // 256MB merge buffer cap
		},
		"scorchMergePlanOptions": map[string]any{
			"MaxSegmentsPerTier":   10,
			"MaxSegmentSize":       32000000, // 32MB segments reduce fragmentation
			"TierGrowth":           4.0,
			"SegmentsPerMergeTask": 10,
			"FloorSegmentSize":     2000,
		},
	}

	idx, err := bleve.NewUsing(tmpPath, m, "scorch", "scorch", kvconfig)
	if err != nil {
		return fmt.Errorf("failed to create new bleve index during rebuild: %w", err)
	}
	// Reset closeOnce so the new index can be properly closed on shutdown.
	e.closeOnce = sync.Once{}

	if len(docs) == 0 {
		// Even with no docs, perform the atomic swap so the index directory is valid.
		if err := idx.Close(); err != nil {
			return fmt.Errorf("failed to close empty staging index: %w", err)
		}
		bakPath := e.path + ".bak"
		bestEffortRemoveAll(bakPath)
		if _, statErr := os.Stat(e.path); statErr == nil {
			bestEffortRename(e.path, bakPath)
		}
		if err := os.Rename(tmpPath, e.path); err != nil {
			if _, statErr := os.Stat(bakPath); statErr == nil {
				bestEffortRename(bakPath, e.path)
			}
			return fmt.Errorf("failed to swap empty rebuilt index into place: %w", err)
		}

		reopenedIdx, err := bleve.Open(e.path)
		if err != nil {
			if _, statErr := os.Stat(bakPath); statErr == nil {
				bestEffortRemoveAll(e.path)
				bestEffortRename(bakPath, e.path)
			}
			return fmt.Errorf("failed to reopen empty rebuilt index: %w", err)
		}

		bestEffortRemoveAll(bakPath)

		e.index = reopenedIdx
		e.closeOnce = sync.Once{}
		slog.Info("BleveEngine: rebuild complete (empty index)")
		return nil
	}

	// Chunk at configurable docs per batch to prevent cold-start memory spikes.
	rebuildBatchSize := e.rebuildBatchSize
	if rebuildBatchSize <= 0 {
		rebuildBatchSize = 100
	}
	batch := idx.NewBatch()
	count := 0
	for id, doc := range docs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := batch.Index(id, toBleveDoc(doc)); err != nil {
			slog.Warn("BleveEngine: failed to add doc to rebuild batch", "id", id, "error", err)
		}
		count++
		if count%rebuildBatchSize == 0 {
			if err := idx.Batch(batch); err != nil {
				return fmt.Errorf("failed to execute rebuild batch chunk: %w", err)
			}
			batch = idx.NewBatch()
		}
	}

	// Flush remaining docs in staging index before swap.
	if batch.Size() > 0 {
		if err := idx.Batch(batch); err != nil {
			return fmt.Errorf("failed to execute final rebuild batch: %w", err)
		}
	}

	// 🛡️ Atomic swap: close staging index, swap directories, reopen at final path.
	if err := idx.Close(); err != nil {
		return fmt.Errorf("failed to close staging index: %w", err)
	}

	// Remove old index directory and move staging into place.
	bakPath := e.path + ".bak"
	bestEffortRemoveAll(bakPath)
	if _, statErr := os.Stat(e.path); statErr == nil {
		if err := os.Rename(e.path, bakPath); err != nil {
			slog.Warn("BleveEngine: failed to backup old index", "error", err)
			bestEffortRemoveAll(e.path)
		}
	}
	if err := os.Rename(tmpPath, e.path); err != nil {
		// Rollback on swap failure
		if _, statErr := os.Stat(bakPath); statErr == nil {
			bestEffortRename(bakPath, e.path)
		}
		return fmt.Errorf("failed to swap rebuilt index into place: %w", err)
	}

	// Reopen at the canonical path.
	reopenedIdx, err := bleve.Open(e.path)
	if err != nil {
		// Rollback on open failure
		if _, statErr := os.Stat(bakPath); statErr == nil {
			bestEffortRemoveAll(e.path)
			bestEffortRename(bakPath, e.path)
		}
		return fmt.Errorf("failed to reopen rebuilt index: %w", err)
	}

	bestEffortRemoveAll(bakPath) // Success, clean up backup

	e.index = reopenedIdx
	e.closeOnce = sync.Once{}

	slog.Info("BleveEngine: rebuild complete", "indexed", len(docs))
	return nil
}

// Index adds or updates a single document in the Bleve index.
func (e *BleveEngine) Index(id string, doc *Document) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.index == nil {
		return fmt.Errorf("bleve engine is closed")
	}
	if err := e.index.Index(id, toBleveDoc(doc)); err != nil {
		return fmt.Errorf("bleve index failed for %q: %w", id, err)
	}
	return nil
}

// IndexBatch atomically indexes multiple documents in a single segment flush.
func (e *BleveEngine) IndexBatch(docs map[string]*Document) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	batch := e.index.NewBatch()
	for id, doc := range docs {
		if err := batch.Index(id, toBleveDoc(doc)); err != nil {
			slog.Warn("BleveEngine: failed to add doc to batch", "id", id, "error", err)
		}
	}

	if err := e.index.Batch(batch); err != nil {
		return fmt.Errorf("bleve batch index failed: %w", err)
	}
	return nil
}

// Delete removes a single document from the Bleve index.
func (e *BleveEngine) Delete(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.index == nil {
		return fmt.Errorf("bleve engine is closed")
	}
	if err := e.index.Delete(id); err != nil {
		return fmt.Errorf("bleve delete failed for %q: %w", id, err)
	}
	return nil
}

// DeleteBatch atomically removes multiple documents from the index.
func (e *BleveEngine) DeleteBatch(ids []string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.index == nil {
		return fmt.Errorf("bleve engine is closed")
	}
	batch := e.index.NewBatch()
	for _, id := range ids {
		batch.Delete(id)
	}

	if err := e.index.Batch(batch); err != nil {
		return fmt.Errorf("bleve batch delete failed: %w", err)
	}
	return nil
}

// Search performs dual-engine search:
//  1. Bleve content/category/tag query → IDs + BM25 scores
//  2. sahilm/fuzzy subsequence match on keys → IDs + fuzzy scores
//  3. Merge, deduplicate, normalize scores to [0,1], sort descending
func (e *BleveEngine) Search(ctx context.Context, q string, keys []string, limit int) ([]SearchHit, error) {
	if q == "" {
		return nil, nil
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	// Channel-based parallel execution.
	type engineResult struct {
		hits []SearchHit
		err  error
	}

	bleveCh := make(chan engineResult, 1)
	fuzzyCh := make(chan engineResult, 1)

	// 1. Bleve content search (goroutine).
	go func(c context.Context) {
		select {
		case <-c.Done():
			bleveCh <- engineResult{nil, c.Err()}
		default:
			hits, err := e.bleveSearch(q, limit)
			bleveCh <- engineResult{hits, err}
		}
	}(ctx)

	// 2. Fuzzy key discovery (goroutine).
	go func(c context.Context) {
		select {
		case <-c.Done():
			fuzzyCh <- engineResult{nil, c.Err()}
		default:
			hits := e.fuzzySearch(q, keys)
			fuzzyCh <- engineResult{hits, nil}
		}
	}(ctx)

	// Wait for both goroutines to finish to ensure they don't access engine fields after Search exits
	bleveRes := <-bleveCh
	fuzzyRes := <-fuzzyCh

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if bleveRes.err != nil {
		slog.Warn("BleveEngine: content search failed, falling back to fuzzy only", "error", bleveRes.err)
	}

	merged := mergeHits(bleveRes.hits, fuzzyRes.hits)

	// Sort by score descending.
	slices.SortFunc(merged, func(a, b SearchHit) int {
		if a.Score < b.Score {
			return 1
		}
		if a.Score > b.Score {
			return -1
		}
		return 0
	})

	if limit > 0 && len(merged) > limit {
		merged = merged[:limit]
	}

	return merged, nil
}

// SearchScoped performs a highly precise boolean query exclusively against
// the Bleve inverted index without sahilm/fuzzy fallback. It builds a
// Conjunction (AND) query where the document must match the textual query (BM25),
// MUST fall into at least one of the provided categories, and MUST contain
// exactly all of the required tags.
func (e *BleveEngine) SearchScoped(ctx context.Context, q string, categories []string, requiredTags []string, limit int) ([]SearchHit, error) {
	if q == "" {
		return nil, nil
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	// 1. Core text/BM25 query
	bq := e.buildQuery(q)
	conj := bleve.NewConjunctionQuery(bq)

	// 2. Category boundary (MUST match at least one of the provided categories)
	if len(categories) > 0 {
		var catQueries []query.Query
		for _, cat := range categories {
			tq := bleve.NewTermQuery(cat)
			tq.SetField("category")
			catQueries = append(catQueries, tq)
		}
		conj.AddQuery(bleve.NewDisjunctionQuery(catQueries...))
	}

	// 3. Dimensional constraints (MUST match every required tag)
	for _, tag := range requiredTags {
		tq := bleve.NewTermQuery(tag)
		tq.SetField("tags")
		conj.AddQuery(tq)
	}

	searchLimit := limit
	if searchLimit <= 0 {
		searchLimit = 100
	}

	req := bleve.NewSearchRequestOptions(conj, searchLimit, 0, false)
	req.Highlight = bleve.NewHighlight()
	res, err := e.index.Search(req)
	if err != nil {
		return nil, fmt.Errorf("scoped bleve search failed: %w", err)
	}

	if len(res.Hits) == 0 {
		return nil, nil
	}

	maxScore := res.Hits[0].Score
	if maxScore <= 0 {
		maxScore = 1.0
	}

	hits := make([]SearchHit, 0, len(res.Hits))
	for _, h := range res.Hits {
		var snips []string
		if h.Fragments != nil {
			for _, frags := range h.Fragments {
				snips = append(snips, frags...)
			}
		}
		hits = append(hits, SearchHit{
			ID:       h.ID,
			Score:    h.Score / maxScore,
			Source:   "bleve_scoped",
			Snippets: snips,
		})
	}

	return hits, nil
}

// SearchScopedSeq provides a zero-allocation iterator over Bleve search hits.
func (e *BleveEngine) SearchScopedSeq(ctx context.Context, q string, categories []string, requiredTags []string, limit int) iter.Seq2[string, float64] {
	return func(yield func(string, float64) bool) {
		if q == "" {
			return
		}

		e.mu.RLock()
		defer e.mu.RUnlock()

		bq := e.buildQuery(q)
		conj := bleve.NewConjunctionQuery(bq)

		if len(categories) > 0 {
			var catQueries []query.Query
			for _, cat := range categories {
				tq := bleve.NewTermQuery(cat)
				tq.SetField("category")
				catQueries = append(catQueries, tq)
			}
			conj.AddQuery(bleve.NewDisjunctionQuery(catQueries...))
		}

		for _, tag := range requiredTags {
			tq := bleve.NewTermQuery(tag)
			tq.SetField("tags")
			conj.AddQuery(tq)
		}

		searchLimit := limit
		if searchLimit <= 0 {
			searchLimit = 100
		}

		req := bleve.NewSearchRequestOptions(conj, searchLimit, 0, false)
		res, err := e.index.Search(req)
		if err != nil || len(res.Hits) == 0 {
			return
		}

		maxScore := res.Hits[0].Score
		if maxScore <= 0 {
			maxScore = 1.0
		}

		for _, h := range res.Hits {
			if !yield(h.ID, h.Score/maxScore) {
				return
			}
		}
	}
}

// bleveSearch runs a Bleve query across content (boosted), category, and tags.
func (e *BleveEngine) bleveSearch(q string, limit int) ([]SearchHit, error) {
	bq := e.buildQuery(q)

	searchLimit := limit
	if searchLimit <= 0 {
		searchLimit = 100
	}

	req := bleve.NewSearchRequestOptions(bq, searchLimit, 0, false)
	req.Highlight = bleve.NewHighlight()
	res, err := e.index.Search(req)
	if err != nil {
		return nil, err
	}

	if len(res.Hits) == 0 {
		return nil, nil
	}

	// Find max score for normalization.
	maxScore := res.Hits[0].Score
	if maxScore <= 0 {
		maxScore = 1.0
	}

	hits := make([]SearchHit, 0, len(res.Hits))
	for _, h := range res.Hits {
		var snips []string
		if h.Fragments != nil {
			for _, frags := range h.Fragments {
				snips = append(snips, frags...)
			}
		}
		hits = append(hits, SearchHit{
			ID:       h.ID,
			Score:    h.Score / maxScore, // Normalize to [0, 1]
			Source:   engineBleve,
			Snippets: snips,
		})
	}

	return hits, nil
}

// fuzzySearch runs sahilm/fuzzy subsequence matching on key names.
func (e *BleveEngine) fuzzySearch(q string, keys []string) []SearchHit {
	if len(keys) == 0 {
		return nil
	}

	matches := fuzzy.Find(q, keys)
	if len(matches) == 0 {
		return nil
	}

	// Find max score for normalization.
	maxScore := matches[0].Score
	if maxScore <= 0 {
		maxScore = 1
	}

	hits := make([]SearchHit, 0, len(matches))
	for _, m := range matches {
		if m.Score <= 0 {
			continue
		}
		hits = append(hits, SearchHit{
			ID:     m.Str,
			Score:  float64(m.Score) / float64(maxScore), // Normalize to [0, 1]
			Source: modeFuzzy,
		})
	}

	return hits
}

// buildQuery constructs a DisjunctionQuery across content (boosted), category,
// and tags for broad BM25-scored matching.
func (e *BleveEngine) buildQuery(q string) query.Query {
	titleQ := bleve.NewMatchQuery(q)
	titleQ.SetField("title")
	titleQ.SetBoost(5.0)

	symbolQ := bleve.NewMatchQuery(q)
	symbolQ.SetField("symbolname")
	symbolQ.SetBoost(5.0)

	contentQ := bleve.NewMatchQuery(q)
	contentQ.SetField("content")
	contentQ.SetBoost(1.0)

	catQ := bleve.NewMatchQuery(q)
	catQ.SetField("category")
	catQ.SetBoost(3.0)

	tagQ := bleve.NewMatchQuery(q)
	tagQ.SetField("tags")
	tagQ.SetBoost(3.0)

	return bleve.NewDisjunctionQuery(titleQ, symbolQ, contentQ, catQ, tagQ)
}

// mergeHits merges and deduplicates hits from Bleve and fuzzy engines.
// If a document appears in both, the higher score is kept and Source is modeBoth.
func mergeHits(bleveHits, fuzzyHits []SearchHit) []SearchHit {
	seen := make(map[string]*SearchHit, len(bleveHits)+len(fuzzyHits))

	for i := range bleveHits {
		h := bleveHits[i]
		seen[h.ID] = &h
	}

	for _, h := range fuzzyHits {
		if existing, ok := seen[h.ID]; ok {
			// Document in both engines — keep max score, mark as modeBoth.
			if h.Score > existing.Score {
				existing.Score = h.Score
			}
			existing.Source = modeBoth
		} else {
			hCopy := h
			seen[h.ID] = &hCopy
		}
	}

	merged := make([]SearchHit, 0, len(seen))
	for _, h := range seen {
		merged = append(merged, *h)
	}

	return merged
}

// DocCount returns the number of documents in the Bleve index.
func (e *BleveEngine) DocCount() (uint64, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.index == nil {
		return 0, nil
	}
	return e.index.DocCount()
}

// Has gracefully checks if a document natively exists in the Bleve index.
func (e *BleveEngine) Has(id string) (bool, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.index == nil {
		return false, nil
	}

	doc, err := e.index.Document(id)
	if err != nil {
		return false, err
	}
	return doc != nil, nil
}

// Close releases the Bleve index resources. Safe to call multiple times;
// only the first invocation performs the actual close.
func (e *BleveEngine) Close() error {
	var closeErr error
	e.closeOnce.Do(func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.index != nil {
			closeErr = e.index.Close()
			e.index = nil
		}
	})
	return closeErr
}
