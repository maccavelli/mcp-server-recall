// Package search provides functionality for the search subsystem.
package search

import (
	"context"
)

// Document represents the searchable fields of a memory record.
// This is a lightweight type decoupled from memory.Record to avoid
// an import cycle between the search and memory packages.
type Document struct {
	Title      string
	SymbolName string
	Content    string
	Category   string
	Tags       []string
	SourcePath string
	SourceHash string
}

// SearchEngine defines the contract for the dual-engine search layer.
// Bleve handles full-text content search (BM25); sahilm/fuzzy handles key
// discovery (character subsequence matching). Both result sets are merged.
type SearchEngine interface {
	// Rebuild re-indexes all documents from the source of truth using Batch
	// internally. Called on cold-start after BadgerDB opens.
	Rebuild(ctx context.Context, docs map[string]*Document) error

	// Index adds or updates a single document in the Bleve index.
	// Called on Save() write-through.
	Index(id string, doc *Document) error

	// IndexBatch atomically indexes multiple documents in a single segment
	// flush. Used by SaveBatch() and Rebuild() to avoid per-record lock
	// contention.
	IndexBatch(docs map[string]*Document) error

	// Delete removes a single document from the Bleve index.
	// Called on Delete() write-through.
	Delete(id string) error

	// DeleteBatch atomically removes multiple documents from the index.
	// Used by consolidation cleanup and batch delete paths.
	DeleteBatch(ids []string) error

	// Search performs a dual-engine search:
	//   1. Bleve query on content/category/tags → IDs + BM25 scores
	//   2. sahilm/fuzzy subsequence match on keys → IDs + fuzzy scores
	//   3. Merge + deduplicate, normalize scores to [0,1]
	// The keys parameter provides the full list of known keys for fuzzy matching.
	Search(ctx context.Context, query string, keys []string, limit int) ([]SearchHit, error)

	// DocCount returns the number of documents currently in the Bleve index.
	// Used for drift-detection audits against BadgerDB record count.
	DocCount() (uint64, error)

	// Has efficiently checks if a document ID exists natively in the index.
	Has(id string) (bool, error)

	// SearchScoped performs a highly precise boolean query exclusively against
	// the Bleve inverted index without sahilm/fuzzy fallback. It builds a
	// Conjunction (AND) query where the document must match the textual query (BM25),
	// MUST fall into at least one of the provided categories, and MUST contain
	// exactly all of the required tags.
	// Used by the standards domain to prevent memory namespace pollution.
	SearchScoped(ctx context.Context, q string, categories []string, requiredTags []string, limit int) ([]SearchHit, error)

	// IsRebuilding returns true if the engine is currently performing a full
	// index rebuild. Used by the audit worker to suppress phantom drift alerts
	// during rebuild windows.
	IsRebuilding() bool

	// Close releases all index resources.
	Close() error
}

// SearchHit represents a single search result with provenance tracking.
type SearchHit struct {
	ID       string   `json:"id"`
	Score    float64  `json:"score"`
	Source   string   `json:"source"` // engineBleve, modeFuzzy, or modeBoth
	Snippets []string `json:"snippets,omitempty,omitzero"`
}
