// Package memory provides functionality for the memory subsystem.
package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
)

func mustZstdDecoder() *zstd.Decoder {
	dec, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(0))
	if err != nil {
		panic(fmt.Errorf("init zstd decoder: %w", err))
	}
	return dec
}

// Domain constants for namespace separation.
const (
	DomainMemories           = "memories"
	DomainStandards          = "standards"
	DomainSessions           = "sessions"
	DomainProjects           = "projects"
	DomainDialecticHistory   = "dialectic_history"
	DomainServerStatus       = "server_status"
	DomainModernizerVerdicts = "modernizer_verdicts" // BUG-2: Socratic gate verdicts for modernizer pipeline
	DomainModernizerTrust    = "modernizer_trust"    // BUG-2: Transform safety metrics for trust scoring
	DomainMADRState          = "madr_state"          // Evolved MADR architectural decision records
	DomainEcosystem          = "ecosystem"
	DomainDocuments          = "documents"
)

// AllDomains defines the centralized list of all known telemetry namespaces.
var AllDomains = []string{
	DomainMemories,
	DomainStandards,
	DomainSessions,
	DomainProjects,
	DomainDialecticHistory,
	DomainServerStatus,
	DomainModernizerVerdicts,
	DomainModernizerTrust,
	DomainMADRState,
	DomainEcosystem,
	DomainDocuments,
}

var (
	zstdEncoderPool = sync.Pool{
		New: func() any {
			enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
			if err != nil {
				panic(fmt.Errorf("init zstd encoder pool: %w", err))
			}
			return enc
		},
	}
	zstdDecoder = mustZstdDecoder()
	zstdMagic   = []byte{0x28, 0xb5, 0x2f, 0xfd}
)

// Record represents a single atomic entry in the memory store with metadata.
type Record struct {
	Title      string    `json:"title,omitempty,omitzero"`
	SymbolName string    `json:"symbolname,omitempty,omitzero"`
	Content    string    `json:"content"`
	Category   string    `json:"category,omitempty,omitzero"`   // Primary classification
	Domain     string    `json:"domain,omitempty,omitzero"`     // Namespace: "memories" or "standards"
	SessionID  string    `json:"session_id,omitempty,omitzero"` // Telemetry binding
	Tags       []string  `json:"tags,omitempty,omitzero"`
	SourcePath string    `json:"source_path,omitempty,omitzero"`
	SourceHash string    `json:"source_hash,omitempty,omitzero"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ReplacementChunk defines an exact-match replacement payload for in-place text mutation.
type ReplacementChunk struct {
	Target        string `json:"target"`
	Replacement   string `json:"replacement"`
	AllowMultiple bool   `json:"allow_multiple,omitempty,omitzero"`
}

// SearchResult wraps a Record with its original key and an optional relevance score.
type SearchResult struct {
	Key         string   `json:"key"`
	Record      *Record  `json:"record,omitempty,omitzero"`
	Score       int      `json:"score,omitempty,omitzero"`
	Summary     string   `json:"summary,omitempty,omitzero"`
	IsTruncated bool     `json:"is_truncated,omitempty,omitzero"`
	Snippets    []string `json:"snippets,omitempty,omitzero"`
}

// marshalRecord centralizes the serialization and Zstd compression of a Record.
func marshalRecord(rec *Record) ([]byte, error) {
	data, err := json.Marshal(rec)
	if err != nil {
		return nil, err
	}
	rawEnc := zstdEncoderPool.Get()
	enc, ok := rawEnc.(*zstd.Encoder)
	if !ok {
		return nil, fmt.Errorf("zstd encoder pool returned %T", rawEnc)
	}
	// Compress the JSON byte slice natively (returns compressed bytes with magic header)
	compressed := enc.EncodeAll(data, make([]byte, 0, len(data)))
	// Size-gate to prevent heap inflation (Black Swan Mitigation)
	if len(data) <= 512*1024 {
		zstdEncoderPool.Put(enc)
	}
	return compressed, nil
}

// migrateRecord converts legacy string formats to the new Record struct if needed.
// Infers Domain from Category for backward compatibility with pre-domain records.
func migrateRecord(data []byte) (*Record, error) {
	return migrateRecordCtx(context.TODO(), data)
}

func migrateRecordCtx(_ context.Context, data []byte) (*Record, error) {
	// Transparently handle Zstd-compressed records by sniffing the magic bytes
	if bytes.HasPrefix(data, zstdMagic) {
		var err error
		data, err = zstdDecoder.DecodeAll(data, nil)
		if err != nil {
			return nil, err
		}
	}

	var rec Record
	if err := json.Unmarshal(data, &rec); err == nil && rec.Content != "" {
		// Infer Domain for records written before the Domain field existed.
		if rec.Domain == "" {
			if HarvestedCategories[rec.Category] {
				rec.Domain = DomainStandards
			} else {
				rec.Domain = DomainMemories
			}
		}
		return &rec, nil
	}

	// Not valid JSON Record, assume legacy string
	return &Record{
		Content:   string(data),
		Domain:    DomainMemories,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Tags:      []string{"legacy"},
	}, nil
}
