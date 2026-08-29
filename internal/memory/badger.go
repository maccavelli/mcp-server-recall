// Package memory provides functionality for the memory subsystem.
package memory

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/dgraph-io/badger/v4/options"
	"github.com/sahilm/fuzzy"

	"github.com/maccavelli/mcp-server-recall/internal/config"
	"github.com/maccavelli/mcp-server-recall/internal/search"
)

// HarvestedCategories defines categories owned exclusively by the standards domain.
// These are excluded from memory-scoped tools (list_categories, search_memories)
// and used as inclusion filters for standards-scoped tools.
var HarvestedCategories = map[string]bool{
	catHarvestedCode: true,
	"PackageDoc":     true,
	catSysDrift:      true,
}

// CacheMetrics defines the CacheMetrics structure.
type CacheMetrics struct {
	Hits       uint64         `json:"hits"`
	Misses     uint64         `json:"misses"`
	DBHits     uint64         `json:"db_hits"`
	DBMisses   uint64         `json:"db_misses"`
	Entries    int            `json:"entries"`
	Namespaces map[string]int `json:"namespaces"`
	BleveDocs  uint64         `json:"bleve_docs"`
}

// QueryRecord stores a single search query event for telemetry tracking.
type QueryRecord struct {
	Query     string
	Timestamp time.Time
	LatencyMs int64
}

// QueryStat aggregates search query telemetry for dashboard display.
type QueryStat struct {
	Query        string  `json:"query"`
	Count        int     `json:"count"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
}

// PrimitiveStat tracks performance metrics for core storage operations.
type PrimitiveStat struct {
	Count          uint64  `json:"count"`
	TotalLatencyMs int64   `json:"-"`
	OpsSec         float64 `json:"ops_sec"`
	EMALatency     float64 `json:"ema_latency"`
}

// MemoryStore manages the BadgerDB persistent storage for memories.
type MemoryStore struct {
	db              *badger.DB
	mu              sync.RWMutex
	ctx             context.Context // Parent context for background workers
	stopGC          chan struct{}
	closeOnce       sync.Once
	cleanup         runtime.Cleanup
	dbClosed        *atomic.Bool        // heap-allocated so AddCleanup can share it without pinning s
	wg              sync.WaitGroup      // Tracks GC and audit goroutines for graceful shutdown
	search          search.SearchEngine // Optional: Bleve full-text search layer
	searchLimit     int                 // Max documents to index
	stopAudit       chan struct{}
	maxBatchSize    int           // Configurable batch size cap
	driftAlerts     atomic.Uint64 // Tracks search index mismatches
	cacheHits       atomic.Uint64
	cacheMisses     atomic.Uint64
	dbHits          atomic.Uint64
	dbMisses        atomic.Uint64
	namespaceCounts map[string]*atomic.Int64
	// New Telemetry Hooks
	gcSweeps           atomic.Uint64
	gcPrunedNodes      atomic.Uint64
	searchLatency      atomic.Int64
	searchQueries      atomic.Uint64
	rpcPayloadBytes    atomic.Uint64
	boundaryViolations atomic.Uint64
	// Write operation breakdown counters
	createOps             atomic.Uint64
	updateOps             atomic.Uint64
	mergeOps              atomic.Uint64
	batchEntriesProcessed atomic.Uint64
	batchErrors           atomic.Uint64
	namespaceEvents       chan string
	// Search query ring buffer (capped at 100)
	queryMu  sync.Mutex
	queryLog []QueryRecord
	// Storage Primitives telemetry
	primMu    sync.RWMutex
	primStats map[string]*PrimitiveStat
}

// GetTelemetry surfaces memory and DB tier metrics.
func (s *MemoryStore) GetTelemetry() (uint64, uint64, uint64, uint64) {
	return s.cacheHits.Load(), s.cacheMisses.Load(), s.dbHits.Load(), s.dbMisses.Load()
}

// GetDBSize returns the BadgerDB LSM and Value log sizes.
func (s *MemoryStore) GetDBSize() (lsm int64, vlog int64) {
	if s.db == nil {
		return 0, 0
	}
	return s.db.Size()
}

// GetExtendedTelemetry exports the new dashboard observability counters.
//
//nolint:gocritic // tuple return avoids allocating a metrics struct on hot telemetry paths.
func (s *MemoryStore) GetExtendedTelemetry() (uint64, uint64, int64, uint64, uint64, uint64) {
	return s.gcSweeps.Load(), s.gcPrunedNodes.Load(), s.searchLatency.Load(), s.searchQueries.Load(), s.rpcPayloadBytes.Load(), s.boundaryViolations.Load()
}

// GetWriteOps returns the cumulative write operation counts.
func (s *MemoryStore) GetWriteOps() (create, update, merge uint64) {
	return s.createOps.Load(), s.updateOps.Load(), s.mergeOps.Load()
}

// GetBatchHealth returns cumulative batch processing metrics.
func (s *MemoryStore) GetBatchHealth() (processed, batchErrors uint64) {
	return s.batchEntriesProcessed.Load(), s.batchErrors.Load()
}

// RecordSearchTelemetry tracks HNSW/Bleve query performance and records the query text.
func (s *MemoryStore) RecordSearchTelemetry(query string, latencyMs int64) {
	s.searchQueries.Add(1)
	s.searchLatency.Add(latencyMs)

	s.queryMu.Lock()
	s.queryLog = append(s.queryLog, QueryRecord{
		Query:     query,
		Timestamp: time.Now(),
		LatencyMs: latencyMs,
	})
	if len(s.queryLog) > 100 {
		s.queryLog = s.queryLog[len(s.queryLog)-100:]
	}
	s.queryMu.Unlock()
}

// RecordPrimitiveLatency tracks the count and latency of a core storage operation.
// EMA (α=0.1) is computed on write to weight recent observations more heavily.
func (s *MemoryStore) RecordPrimitiveLatency(operation string, start time.Time) {
	latency := float64(time.Since(start).Milliseconds())
	s.primMu.Lock()
	defer s.primMu.Unlock()
	if s.primStats == nil {
		s.primStats = make(map[string]*PrimitiveStat)
	}
	stat, exists := s.primStats[operation]
	if !exists {
		stat = &PrimitiveStat{EMALatency: latency}
		s.primStats[operation] = stat
	}
	stat.Count++
	stat.TotalLatencyMs += int64(latency)
	// Exponential Moving Average: α=0.1 weights recent ~10 observations most.
	const alpha = 0.1
	stat.EMALatency = alpha*latency + (1-alpha)*stat.EMALatency
}

// GetPrimitiveMetrics exports primitive metrics, computing Ops/sec based on uptime.
func (s *MemoryStore) GetPrimitiveMetrics(uptimeSec int64) map[string]PrimitiveStat {
	s.primMu.RLock()
	defer s.primMu.RUnlock()
	out := make(map[string]PrimitiveStat)
	if s.primStats == nil {
		return out
	}
	for op, stat := range s.primStats {
		opsSec := float64(0)
		if uptimeSec > 0 {
			opsSec = float64(stat.Count) / float64(uptimeSec)
		}
		out[op] = PrimitiveStat{
			Count:          stat.Count,
			TotalLatencyMs: stat.TotalLatencyMs,
			OpsSec:         opsSec,
			EMALatency:     stat.EMALatency,
		}
	}
	return out
}

// GetTTLHorizon calculates entities nearing expiration based on the purge threshold.
func (s *MemoryStore) GetTTLHorizon(ctx context.Context, purgeDays int) (h24 int, h7d int, h30d int) {
	sessionsSeq, err := s.ListSessions(ctx, "", "", "", "", "", 0)
	if err != nil {
		return
	}
	purgeDuration := time.Duration(purgeDays) * 24 * time.Hour
	for sess := range sessionsSeq {
		timeUntilPurge := purgeDuration - time.Since(sess.Record.UpdatedAt)
		if timeUntilPurge < 0 {
			h24++
			h7d++
			h30d++
		} else if timeUntilPurge <= 24*time.Hour {
			h24++
			h7d++
			h30d++
		} else if timeUntilPurge <= 7*24*time.Hour {
			h7d++
			h30d++
		} else if timeUntilPurge <= 30*24*time.Hour {
			h30d++
		}
	}
	return
}

// GetTopQueries aggregates the query ring buffer and returns the top N queries by count.
func (s *MemoryStore) GetTopQueries(n int) []QueryStat {
	s.queryMu.Lock()
	logCopy := slices.Clone(s.queryLog)
	s.queryMu.Unlock()

	agg := make(map[string]*QueryStat)
	for _, q := range logCopy {
		st, ok := agg[q.Query]
		if !ok {
			st = &QueryStat{Query: q.Query}
			agg[q.Query] = st
		}
		st.Count++
		st.AvgLatencyMs += float64(q.LatencyMs)
	}

	results := make([]QueryStat, 0, len(agg))
	for _, st := range agg {
		st.AvgLatencyMs /= float64(st.Count)
		results = append(results, *st)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Count > results[j].Count
	})

	if len(results) > n {
		results = results[:n]
	}
	return results
}

// RecordRPCBytes tracks gateway ingress/egress.
func (s *MemoryStore) RecordRPCBytes(nBytes uint64) {
	s.rpcPayloadBytes.Add(nBytes)
}

// RecordSecurityViolation tracks access denials.
func (s *MemoryStore) RecordSecurityViolation() {
	s.boundaryViolations.Add(1)
}

// NewMemoryStore initializes a new BadgerDB with optional AES-256 encryption.
func NewMemoryStore(ctx context.Context, dbPath string, encryptionKey string, searchLimit int, batchCfg config.BatchConfig) (*MemoryStore, error) {
	if err := os.MkdirAll(dbPath, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	opts, err := buildBadgerOptions(dbPath, encryptionKey)
	if err != nil {
		return nil, err
	}

	db, err := openBadgerWithRetry(opts, 5)
	if err != nil {
		return nil, err
	}

	checkVlogHealth(dbPath)

	slog.Info("BadgerDB initialized within bounds",
		"sync_writes", false,
		"compression", "zstd",
		"memtable_mb", 32,
		"block_cache_mb", 256,
		"index_cache_mb", 256,
	)

	closed := new(atomic.Bool)
	s := &MemoryStore{
		db:              db,
		ctx:             ctx,
		stopGC:          make(chan struct{}),
		stopAudit:       make(chan struct{}),
		dbClosed:        closed,
		searchLimit:     searchLimit,
		maxBatchSize:    batchCfg.MaxBatchSize,
		namespaceCounts: make(map[string]*atomic.Int64),
		primStats:       make(map[string]*PrimitiveStat),
		namespaceEvents: make(chan string, 1000), // Non-blocking buffer
	}

	for _, domain := range AllDomains {
		s.namespaceCounts[domain] = &atomic.Int64{}
	}

	// Safety net for leaked stores. Close() must Stop this and take the same
	// dbClosed CAS; otherwise GC closes the fd after Close and the next open
	// (or the explicit Close) sees MANIFEST: bad file descriptor.
	s.cleanup = runtime.AddCleanup(s, func(arg dbCloseArg) {
		if !arg.closed.CompareAndSwap(false, true) {
			return
		}
		slog.Warn("MemoryStore garbage collected, forcefully closing BadgerDB mmaps")
		_ = arg.db.Close() //nolint:errcheck // cleanup hook; close errors are non-actionable during GC
	}, dbCloseArg{db: db, closed: closed})

	// Start background maintenance
	s.wg.Go(func() {
		s.runGC()
	})
	if searchLimit > 0 {
		s.wg.Go(func() {
			s.runAuditWorker()
		})
	}

	slog.Info("MemoryStore initialized with maintenance", "path", dbPath, "encrypted", encryptionKey != "")
	return s, nil
}

// dbCloseArg is the AddCleanup payload. closed is allocated off the
// MemoryStore so the cleanup does not pin s alive.
type dbCloseArg struct {
	db     *badger.DB
	closed *atomic.Bool
}

// StartConfigWatcher launches the decoupled goroutine that deduplicates namespace events
// and flushes the active list to .recall-dynamic-namespaces.json, firing the callback to update the schema.
func (s *MemoryStore) StartConfigWatcher(configDir string, onUpdate func()) {
	s.wg.Go(func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		var pending bool
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-s.stopGC:
				return
			case <-s.namespaceEvents:
				pending = true
			case <-ticker.C:
				if pending {
					pending = false
					s.flushNamespaces(configDir)
					if onUpdate != nil {
						onUpdate()
					}
				}
			}
		}
	})
}

func (s *MemoryStore) flushNamespaces(configDir string) {
	s.mu.RLock()
	var active []string
	for ns, count := range s.namespaceCounts {
		if count.Load() > 0 {
			active = append(active, ns)
		}
	}
	s.mu.RUnlock()

	sort.Strings(active)
	b, err := json.MarshalIndent(active, "", "  ")
	if err != nil {
		slog.Error("Failed to marshal dynamic namespaces", "error", err)
		return
	}

	target := filepath.Join(configDir, ".recall-dynamic-namespaces.json")
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		slog.Error("Failed to write temp dynamic namespaces file", "error", err)
		return
	}
	if err := os.Rename(tmp, target); err != nil {
		slog.Error("Failed to commit dynamic namespaces file via rename", "error", err)
		return
	}
}

// buildBadgerOptions constructs the BadgerDB options chain with optional encryption.
func buildBadgerOptions(dbPath string, encryptionKey string) (badger.Options, error) {
	opts := badger.DefaultOptions(dbPath).
		WithLogger(nil).
		WithSyncWrites(false).
		// 🛡️ OPTIMIZATION (10k Bounds): Burst write absorption
		// Decrease memtables to 2 (32MB total buffer) to force LSM flushes and WAL truncation
		// BlockCache increased to 256MB for zero-read-I/O performance
		WithValueLogMaxEntries(50000).
		WithValueLogFileSize(64 << 20).
		WithBlockSize(4096).
		WithMemTableSize(16 << 20).
		WithNumMemtables(2).
		WithIndexCacheSize(256 << 20).
		WithBlockCacheSize(256 << 20).
		WithBaseTableSize(8 << 20).
		WithBaseLevelSize(20 << 20).
		WithLevelSizeMultiplier(10).
		WithMaxLevels(7).
		// Default ValueThreshold prevents index bloat
		WithValueThreshold(1 << 10). // 1KB
		WithNumLevelZeroTables(10).
		WithNumLevelZeroTablesStall(20).
		WithCompactL0OnClose(true).
		WithChecksumVerificationMode(options.OnTableRead).
		// 🛡️ CPU CAP: Native 2-Core Topology Match. Prevents heavy context switching.
		WithNumGoroutines(2).
		WithMetricsEnabled(true)

	if encryptionKey != "" {
		binaryKey, err := hex.DecodeString(encryptionKey)
		if err != nil {
			return opts, fmt.Errorf("failed to decode hex encryption key: %w", err)
		}
		if len(binaryKey) != 16 && len(binaryKey) != 24 && len(binaryKey) != 32 {
			return opts, fmt.Errorf("encryption key must be exactly 16, 24, or 32 bytes (got %d)", len(binaryKey))
		}
		opts = opts.WithEncryptionKey(binaryKey).
			WithEncryptionKeyRotationDuration(7 * 24 * time.Hour)
	}

	return opts, nil
}

// openBadgerWithRetry attempts to open BadgerDB with exponential backoff for lock contention.
func openBadgerWithRetry(opts badger.Options, maxRetries int) (*badger.DB, error) {
	var db *badger.DB
	var err error

	backoff := 500 * time.Millisecond
	for i := range maxRetries {
		db, err = badger.Open(opts)
		if err == nil {
			return db, nil
		}
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "cannot acquire directory lock") ||
			strings.Contains(errStr, "another process is using this file") ||
			strings.Contains(errStr, "resource temporarily unavailable") ||
			strings.Contains(errStr, "lock acquire") ||
			strings.Contains(errStr, "acquire lock") {
			slog.Warn("Badger directory lock held; retrying...", "attempt", i+1, "max_retries", maxRetries, "backoff", backoff)
			time.Sleep(backoff)
			backoff *= 2
			continue
		}
		return nil, fmt.Errorf("failed to open badger db: %w", err)
	}

	return nil, fmt.Errorf("failed to open badger db after %d retries: %w", maxRetries, err)
}

// checkVlogHealth warns if any vlog file has grown excessively (indicates GC failure).
func checkVlogHealth(dbPath string) {
	entries, err := os.ReadDir(dbPath)
	if err != nil {
		return
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".vlog") {
			if info, err := e.Info(); err == nil && info.Size() > 300<<20 {
				slog.Warn("Bloated vlog detected — consider DB reset",
					"file", e.Name(), "size_mb", info.Size()>>20)
			}
		}
	}
}

// SetSearchEngine attaches a SearchEngine and performs a cold-start rebuild.
// Must be called after NewMemoryStore and before serving requests.
func (s *MemoryStore) SetSearchEngine(ctx context.Context, engine search.SearchEngine) error {
	s.mu.Lock()
	s.search = engine
	s.mu.Unlock()

	return s.SyncSearchIndex(ctx)
}

// SyncSearchIndex performs a full scan of BadgerDB and rebuilds the search index.
// This is used for cold-starts and the reload_cache runtime tool.
//
//nolint:gocognit // SyncSearchIndex coordinates multi-phase Badger/index maintenance.
func (s *MemoryStore) SyncSearchIndex(ctx context.Context) error {
	s.mu.RLock()
	searchEngine := s.search
	s.mu.RUnlock()

	if searchEngine == nil {
		return fmt.Errorf("search engine not initialized")
	}

	// Resource Safety: Check document count against memory limit.
	// We use the BadgerDB key count as a heuristic before rebuilding.
	count, _, err := s.GetStats()
	if err == nil && s.searchLimit > 0 && count > s.searchLimit {
		slog.Warn("Search memory limit exceeded; falling back to fuzzy matching",
			"count", count, "limit", s.searchLimit)
		s.mu.Lock()
		s.search = nil // Disable Bleve for this session
		s.mu.Unlock()
		return nil
	}

	// Cold-start: rebuild the full-text index from BadgerDB.
	for _, counter := range s.namespaceCounts {
		counter.Store(0)
	}
	docs := make(map[string]*search.Document)
	err = s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			k := string(it.Item().Key())
			if strings.HasPrefix(k, "_idx:") {
				continue
			}
			if err := it.Item().Value(func(v []byte) error {
				rec, mErr := migrateRecord(v)
				if mErr != nil {
					return err
				}
				if counter, exists := s.namespaceCounts[rec.Domain]; exists {
					counter.Add(1)
				} else {
					newCounter := &atomic.Int64{}
					newCounter.Add(1)
					s.namespaceCounts[rec.Domain] = newCounter
					select {
					case s.namespaceEvents <- rec.Domain:
					default:
					}
				}
				// Inject synthetic domain tag so SearchScoped conjunction queries match
				indexTags := append(slices.Clone(rec.Tags), "domain:"+rec.Domain)
				if after, ok := strings.CutPrefix(k, "pkg:"); ok {
					parts := strings.SplitN(after, ":", 2)
					if len(parts) >= 1 {
						indexTags = append(indexTags, "package:"+parts[0])
					}
				}
				docs[k] = &search.Document{
					Title:      rec.Title,
					SymbolName: rec.SymbolName,
					Content:    rec.Content,
					Category:   rec.Category,
					Tags:       indexTags,
					SourcePath: rec.SourcePath,
					SourceHash: rec.SourceHash,
				}
				return nil
			}); err != nil {
				slog.Warn("Skipping corrupted record during search rebuild", "key", k, "error", err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to read records for search rebuild: %w", err)
	}

	// Fix #15: Domain index backfill for legacy records.
	// Records written before domain indices were introduced lack _idx:domain: entries.
	// Without this backfill, accelerated prefix scans (Fixes #6-8) would miss legacy records.
	var backfillKeys []struct {
		domIdx string
		key    string
	}
	if bfErr := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			k := string(it.Item().Key())
			if strings.HasPrefix(k, "_idx:") {
				continue
			}
			if vErr := it.Item().Value(func(v []byte) error {
				rec, mErr := migrateRecord(v)
				if mErr == nil && rec != nil && rec.Domain != "" {
					domIdx := fmt.Sprintf("_idx:domain:%s:%s", rec.Domain, k)
					// Check if index entry already exists
					if _, gErr := txn.Get([]byte(domIdx)); errors.Is(gErr, badger.ErrKeyNotFound) {
						backfillKeys = append(backfillKeys, struct {
							domIdx string
							key    string
						}{domIdx: domIdx, key: k})
					}
				}
				return nil
			}); vErr != nil {
				continue
			}
		}
		return nil
	}); bfErr != nil {
		slog.Warn("Domain index backfill scan failed (non-fatal)", "error", bfErr)
	}

	if len(backfillKeys) > 0 {
		const backfillChunkSize = 100
		for i := 0; i < len(backfillKeys); i += backfillChunkSize {
			end := min(i+backfillChunkSize, len(backfillKeys))
			chunk := backfillKeys[i:end]
			if wErr := s.UpdateWithRetry(func(txn *badger.Txn) error {
				for _, entry := range chunk {
					if err := txn.Set([]byte(entry.domIdx), []byte(entry.key)); err != nil {
						return err
					}
				}
				return nil
			}); wErr != nil {
				slog.Error("Domain index backfill chunk failed", "chunkStart", i, "error", wErr)
				break
			}
		}
		slog.Info("Domain index backfill complete", "backfilled", len(backfillKeys))
	}
	if err := searchEngine.Rebuild(ctx, docs); err != nil {
		return fmt.Errorf("search engine rebuild failed: %w", err)
	}

	slog.Info("Search engine synchronization complete", "count", len(docs))
	return nil
}

// runGC executes BadgerDB value log garbage collection periodically.
// Implements a progressive decay matrix to systematically reclaim disk space gracefully.
func (s *MemoryStore) runGC() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	// GC Decay threshold logic (from gentlest to most aggressive)
	thresholds := []float64{0.7, 0.5, 0.3, 0.1}

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			// Progressive threshold fallback
			totalReclaimed := 0
			for _, ratio := range thresholds {
				runCount := 0
				for {
					err := s.db.RunValueLogGC(ratio)
					if err != nil {
						if !errors.Is(err, badger.ErrNoRewrite) && !errors.Is(err, badger.ErrRejected) {
							slog.Debug("Badger GC passed on threshold", "ratio", ratio, "error", err)
						}
						break
					}
					runCount++
				}
				if runCount > 0 {
					totalReclaimed += runCount
					s.gcSweeps.Add(uint64(runCount))
					slog.Info("Badger progressive GC reclaimed disk blocks natively", "ratio", ratio, "cycles", runCount)
				}
			}
			// Only flatten after GC actually reclaimed data to avoid wasted LSM rewrites.
			if totalReclaimed > 0 {
				if err := s.db.Flatten(1); err != nil {
					slog.Warn("Badger Flatten execution failed during GC", "error", err)
				}
			}
		case <-s.stopGC:
			return
		}
	}
}

// runAuditWorker periodically verifies the integrity of the search index.
func (s *MemoryStore) runAuditWorker() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.performAudit()
		case <-s.stopAudit:
			return
		}
	}
}

func (s *MemoryStore) performAudit() {
	s.mu.RLock()
	searchEngine := s.search
	s.mu.RUnlock()

	if searchEngine == nil {
		return
	}

	// Suppress phantom drift alerts during active rebuilds.
	if searchEngine.IsRebuilding() {
		slog.Debug("Audit skipped: search engine is rebuilding")
		return
	}

	// 🛡️ F8: Single-pass reservoir sampling across entire keyspace.
	// Combines counting and sampling into one iteration to halve I/O cost.
	const sampleSize = 5
	var driftedKeys []string
	err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		// Reservoir sampling: maintain a sample set of size sampleSize.
		// For each non-index key at position i, include it with probability sampleSize/i.
		reservoir := make([]string, 0, sampleSize)
		count := 0
		for it.Rewind(); it.Valid(); it.Next() {
			keyStr := string(it.Item().Key())
			if strings.HasPrefix(keyStr, "_idx:") {
				continue
			}
			count++
			if len(reservoir) < sampleSize {
				reservoir = append(reservoir, keyStr)
			} else if rand.IntN(count) < sampleSize { //nolint:gosec // reservoir sampling is non-cryptographic audit sampling
				reservoir[rand.IntN(sampleSize)] = keyStr //nolint:gosec // reservoir sampling is non-cryptographic audit sampling
			}
		}

		// Audit sampled keys
		for _, keyStr := range reservoir {
			if !verifySearchEntry(searchEngine, keyStr) {
				slog.Warn("Search index drift detected by audit worker", "key", keyStr)
				s.driftAlerts.Add(1)
				driftedKeys = append(driftedKeys, keyStr)
			}
		}
		return nil
	})

	if err != nil {
		slog.Error("Search audit worker failed", "error", err)
	}

	if len(driftedKeys) > 0 {
		ctx := s.ctx
		for _, key := range driftedKeys {
			rec, err := s.Get(ctx, key)
			if err != nil {
				continue
			}
			// Inject synthetic domain tag for SearchScoped conjunction match
			indexTags := append(slices.Clone(rec.Tags), "domain:"+rec.Domain)
			doc := &search.Document{
				Title:      rec.Title,
				SymbolName: rec.SymbolName,
				Content:    rec.Content,
				Category:   rec.Category,
				Tags:       indexTags,
				SourcePath: rec.SourcePath,
				SourceHash: rec.SourceHash,
			}
			if sErr := searchEngine.Index(key, doc); sErr == nil {
				slog.Info("Drift healed successfully", "key", key)
			} else {
				slog.Warn("Failed to heal drift", "key", key, "error", sErr)
			}
		}
	}
}

// verifySearchEntry checks if a key exists natively in the search engine index.
func verifySearchEntry(engine search.SearchEngine, key string) bool {
	exists, err := engine.Has(key)
	if err != nil {
		return false
	}
	return exists
}

// DriftAlerts returns the total number of index mismatches detected.
func (s *MemoryStore) DriftAlerts() uint64 {
	return s.driftAlerts.Load()
}

// DocCount returns the number of documents in the Bleve index.
func (s *MemoryStore) DocCount() (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.search == nil {
		return 0, nil
	}
	return s.search.DocCount()
}

func saveRecordNeedsFsync(domain string, tags []string) bool {
	if domain == DomainDialecticHistory {
		return true
	}
	return slices.Contains(tags, "outcome:saved")
}

func (s *MemoryStore) saveRecordBumpNamespace(domain, action string) {
	if action != fieldCreated {
		return
	}
	if counter, exists := s.namespaceCounts[domain]; exists {
		if counter.Add(1) == 1 && domain != DomainMemories {
			select {
			case s.namespaceEvents <- domain:
			default:
			}
		}
		return
	}
	newCounter := &atomic.Int64{}
	newCounter.Add(1)
	s.namespaceCounts[domain] = newCounter
	if domain != DomainMemories {
		select {
		case s.namespaceEvents <- domain:
		default:
		}
	}
}

func indexSaveRecordToSearch(searchEngine search.SearchEngine, key, title, content, category string, tags []string, domain string) {
	indexTags := append(slices.Clone(tags), "domain:"+domain)
	var symName string
	if after, ok := strings.CutPrefix(key, "pkg:"); ok {
		parts := strings.SplitN(after, ":", 2)
		if len(parts) == 2 {
			indexTags = append(indexTags, "package:"+parts[0])
			symName = parts[1]
		} else if len(parts) == 1 {
			indexTags = append(indexTags, "package:"+parts[0])
		}
	}
	doc := &search.Document{Title: title, Content: content, Category: category, Tags: indexTags, SymbolName: symName}
	if sErr := searchEngine.Index(key, doc); sErr != nil {
		slog.Warn("Bleve index update failed (non-fatal)", "key", key, "error", sErr)
	}
}

// SaveResult describes the outcome of a Save operation with dedup metadata.
type SaveResult struct {
	Action    string `json:"action"`                         // fieldCreated, fieldUpdated, or fieldMerged
	Key       string `json:"key"`                            // Final key where the record lives
	MergedKey string `json:"merged_with,omitempty,omitzero"` // Original key that was merged into (if Action=merged)
}

// Save stores or updates a memory Record in the database with optional inline dedup.
// When dedupThreshold > 0 and the key is new, same-category memory-domain records are
// scanned via Jaccard similarity. If a match exceeds the threshold, the incoming content
// merges into the existing record.
//
//nolint:gocognit // Save coordinates multi-phase Badger/index maintenance.
func (s *MemoryStore) Save(ctx context.Context, title, key, content, category string, tags []string, domain string, dedupThreshold float64) (*SaveResult, error) {
	defer s.RecordPrimitiveLatency("create_entities", time.Now())

	if domain == "" {
		domain = DomainMemories
	}

	// Enforce namespace: memory-domain writes must not use standards categories.
	if domain == DomainMemories && HarvestedCategories[category] {
		s.RecordSecurityViolation()
		return nil, fmt.Errorf("category %q is reserved for the standards domain", category)
	}

	s.mu.Lock()

	// Phase 2: Inline dedup — only for new keys in memory domain with threshold > 0.
	// Dedup requires a pre-read to check for existing key, done via separate View
	// since findSimilarLocked needs s.mu and runs before the write transaction.
	var oldRec *Record
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return nil
			}
			return err
		}
		return item.Value(func(val []byte) error {
			if old, err := migrateRecord(val); err == nil {
				oldRec = old
			} else {
				slog.Warn("Failed to migrate record during save", "key", key, "error", err)
			}
			return nil
		})
	})
	if err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("lookup failure during save: %w", err)
	}

	if oldRec == nil && dedupThreshold > 0 && domain == DomainMemories && category != "" {
		if match := s.findSimilarLocked(content, category, dedupThreshold); match != nil {
			// Merge incoming into existing record.
			mergedTags := mergeTags(match.Record.Tags, tags)
			mergedContent := match.Record.Content
			if len(content) > len(mergedContent) {
				mergedContent = content
			}
			mergedTitle := match.Record.Title
			if mergedTitle == "" && title != "" {
				mergedTitle = title
			}

			s.mergeOps.Add(1)
			result, mErr := s.updateRecordLocked(ctx, match.Key, mergedTitle, mergedContent, match.Record.Category, mergedTags, match.Record.Domain, match.Record.CreatedAt)
			s.mu.Unlock()
			return result, mErr
		}
	}

	// Phase 3: Standard write (create or update).
	// Re-read oldRec INSIDE the write transaction to eliminate cross-transaction
	// stale-snapshot vulnerability. txn.Get() creates a read-set entry enabling
	// BadgerDB conflict detection via ErrConflict + automatic retry.
	action := fieldCreated
	now := time.Now()
	var rec *Record
	err = s.UpdateWithRetry(func(txn *badger.Txn) error {
		// Re-read the current record inside the write transaction for snapshot consistency.
		var currentOldRec *Record
		item, gErr := txn.Get([]byte(key))
		if gErr == nil {
			if err := item.Value(func(val []byte) error {
				if old, mErr := migrateRecord(val); mErr == nil {
					currentOldRec = old
				}
				return nil
			}); err != nil {
				return err
			}
		} else if !errors.Is(gErr, badger.ErrKeyNotFound) {
			return gErr
		}

		if currentOldRec != nil {
			action = fieldUpdated
			s.deleteRecordIndices(txn, key, currentOldRec)
		}

		rec = &Record{
			Title:     title,
			Content:   content,
			Category:  category,
			Domain:    domain,
			Tags:      tags,
			UpdatedAt: now,
		}
		if currentOldRec != nil {
			rec.CreatedAt = currentOldRec.CreatedAt
		} else {
			rec.CreatedAt = now
		}

		data, err := marshalRecord(rec)
		if err != nil {
			return err
		}

		entry := badger.NewEntry([]byte(key), data)

		if err := txn.SetEntry(entry); err != nil {
			return fmt.Errorf("failed to set record: %w", err)
		}

		return s.createRecordIndices(txn, key, rec)
	})

	if err != nil {
		s.mu.Unlock()
		return nil, err
	}

	slog.Debug("Memory saved and indexed", "key", key, "category", category, "domain", domain, "tag_count", len(tags))

	// Capture search engine reference under lock for post-unlock Bleve indexing.
	searchEngine := s.search

	s.saveRecordBumpNamespace(domain, action)

	switch action {
	case fieldCreated:
		s.createOps.Add(1)
	case fieldUpdated:
		s.updateOps.Add(1)
	}

	// Fix #10: Targeted db.Sync() for durability on high-value write paths.
	if saveRecordNeedsFsync(domain, tags) {
		if syncErr := s.db.Sync(); syncErr != nil {
			slog.Warn("db.Sync() failed after critical write (data in WAL, not fsync'd)", "key", key, "domain", domain, "error", syncErr)
		}
	}

	// Release lock BEFORE Bleve indexing to reduce reader starvation (~5-15ms savings).
	s.mu.Unlock()

	if searchEngine != nil {
		indexSaveRecordToSearch(searchEngine, key, title, content, category, tags, domain)
	}

	return &SaveResult{Action: action, Key: key}, nil
}

// UpdateRecord performs an atomic, in-place edit of a record's text, metadata, or key.
//
//nolint:gocognit // UpdateRecord coordinates multi-phase Badger/index maintenance.
func (s *MemoryStore) UpdateRecord(ctx context.Context, namespace, key, newKey, title, category string, tags []string, replacements []ReplacementChunk) (*SaveResult, error) {
	defer s.RecordPrimitiveLatency("update_in_place", time.Now())

	s.mu.Lock()

	var rec *Record
	var oldRec *Record
	now := time.Now()

	err := s.UpdateWithRetry(func(txn *badger.Txn) error {
		// 1. Hydrate existing record
		item, gErr := txn.Get([]byte(key))
		if gErr != nil {
			return gErr // returns ErrKeyNotFound if missing
		}

		if vErr := item.Value(func(val []byte) error {
			if parsed, mErr := migrateRecord(val); mErr == nil {
				oldRec = parsed
			} else {
				return mErr
			}
			return nil
		}); vErr != nil {
			return fmt.Errorf("failed to parse existing record: %w", vErr)
		}

		if oldRec == nil {
			return fmt.Errorf("record %q is structurally invalid", key)
		}

		// 2. Collision Guard for Renames
		if newKey != "" && newKey != key {
			_, cErr := txn.Get([]byte(newKey))
			if cErr == nil {
				return fmt.Errorf("target key %q already exists", newKey)
			} else if !errors.Is(cErr, badger.ErrKeyNotFound) {
				return fmt.Errorf("collision check failed: %w", cErr)
			}
		}

		// 3. Surgical Mutations
		rec = &Record{
			Title:      oldRec.Title,
			SymbolName: oldRec.SymbolName,
			Content:    oldRec.Content,
			Category:   oldRec.Category,
			Domain:     oldRec.Domain,
			SessionID:  oldRec.SessionID,
			Tags:       oldRec.Tags,
			SourcePath: oldRec.SourcePath,
			SourceHash: oldRec.SourceHash,
			CreatedAt:  oldRec.CreatedAt,
			UpdatedAt:  now,
		}

		if title != "" {
			rec.Title = title
		}
		if category != "" {
			rec.Category = category
		}
		if len(tags) > 0 {
			rec.Tags = tags
		}

		// Apply string replacements
		for _, chunk := range replacements {
			count := strings.Count(rec.Content, chunk.Target)
			if count == 0 {
				return fmt.Errorf("target string not found in content: %q", chunk.Target)
			}
			if count > 1 && !chunk.AllowMultiple {
				return fmt.Errorf("target string is ambiguous (found %d times) and allow_multiple is false", count)
			}

			if chunk.AllowMultiple {
				rec.Content = strings.ReplaceAll(rec.Content, chunk.Target, chunk.Replacement)
			} else {
				rec.Content = strings.Replace(rec.Content, chunk.Target, chunk.Replacement, 1)
			}
		}

		// 4. Index Lifecycle Management
		targetKey := key
		if newKey != "" && newKey != key {
			targetKey = newKey
		}

		s.deleteRecordIndices(txn, key, oldRec)

		if targetKey != key {
			if dErr := txn.Delete([]byte(key)); dErr != nil {
				return fmt.Errorf("failed to delete old key %q: %w", key, dErr)
			}
		}

		data, mErr := marshalRecord(rec)
		if mErr != nil {
			return fmt.Errorf("failed to marshal mutated record: %w", mErr)
		}

		entry := badger.NewEntry([]byte(targetKey), data)
		if sErr := txn.SetEntry(entry); sErr != nil {
			return fmt.Errorf("failed to set mutated record: %w", sErr)
		}

		return s.createRecordIndices(txn, targetKey, rec)
	})

	if err != nil {
		s.mu.Unlock()
		return nil, err
	}

	searchEngine := s.search
	s.updateOps.Add(1)

	needSync := rec.Domain == DomainDialecticHistory
	if !needSync {
		if slices.Contains(rec.Tags, "outcome:saved") {
			needSync = true
		}
	}
	if needSync {
		if syncErr := s.db.Sync(); syncErr != nil {
			slog.Warn("db.Sync() failed after critical write", "key", key, "error", syncErr)
		}
	}

	s.mu.Unlock()

	// 5. Decoupled Index Sync
	targetKey := key
	if newKey != "" && newKey != key {
		targetKey = newKey
	}

	if searchEngine != nil {
		if targetKey != key {
			if dErr := searchEngine.Delete(key); dErr != nil {
				slog.Warn("Bleve old index delete failed", "key", key, "error", dErr)
			}
		}

		// Replicate native synthetic tag logic from Save()
		indexTags := append(slices.Clone(rec.Tags), "domain:"+rec.Domain)
		var symName string
		if after, ok := strings.CutPrefix(targetKey, "pkg:"); ok {
			parts := strings.SplitN(after, ":", 2)
			if len(parts) == 2 {
				indexTags = append(indexTags, "package:"+parts[0])
				symName = parts[1]
			} else if len(parts) == 1 {
				indexTags = append(indexTags, "package:"+parts[0])
			}
		}

		doc := &search.Document{
			Title:      rec.Title,
			Content:    rec.Content,
			Category:   rec.Category,
			Tags:       indexTags,
			SymbolName: symName,
		}

		if sErr := searchEngine.Index(targetKey, doc); sErr != nil {
			slog.Warn("Bleve index update failed", "key", targetKey, "error", sErr)
		}
	}

	return &SaveResult{Action: fieldUpdated, Key: targetKey}, nil
}

// updateRecordLocked writes a merged record. Caller must hold s.mu.
func (s *MemoryStore) updateRecordLocked(_ context.Context, key, title, content, category string, tags []string, domain string, createdAt time.Time) (*SaveResult, error) {
	now := time.Now()
	err := s.UpdateWithRetry(func(txn *badger.Txn) error {
		// Read and clean old indices.
		item, err := txn.Get([]byte(key))
		if err == nil {
			if vErr := item.Value(func(val []byte) error {
				if old, err := migrateRecord(val); err == nil {
					s.deleteRecordIndices(txn, key, old)
				}
				return nil
			}); vErr != nil {
				slog.Warn("Failed to read old record value during merge", "key", key, "error", vErr)
			}
		}

		rec := &Record{
			Title:     title,
			Content:   content,
			Category:  category,
			Domain:    domain,
			Tags:      tags,
			CreatedAt: createdAt,
			UpdatedAt: now,
		}
		data, err := marshalRecord(rec)
		if err != nil {
			return err
		}

		entry := badger.NewEntry([]byte(key), data)

		if err := txn.SetEntry(entry); err != nil {
			return fmt.Errorf("failed to set merged record: %w", err)
		}
		return s.createRecordIndices(txn, key, rec)
	})
	if err != nil {
		return nil, err
	}

	slog.Info("Dedup merge completed", "merged_into", key)

	if s.search != nil {
		// Inject synthetic domain tag for SearchScoped conjunction match
		indexTags := append(slices.Clone(tags), "domain:"+domain)
		doc := &search.Document{Title: title, Content: content, Category: category, Tags: indexTags}
		if sErr := s.search.Index(key, doc); sErr != nil {
			slog.Warn("Bleve index update failed after dedup merge (non-fatal)", "key", key, "error", sErr)
		}
	}

	return &SaveResult{Action: fieldMerged, Key: key, MergedKey: key}, nil
}

// loadRecordFromTxn reads and migrates a record by key within a read-only transaction.
func loadRecordFromTxn(txn *badger.Txn, key []byte) (*Record, error) {
	item, err := txn.Get(key)
	if err != nil {
		return nil, err
	}
	var rec *Record
	if err := item.Value(func(val []byte) error {
		var mErr error
		rec, mErr = migrateRecord(val)
		return mErr
	}); err != nil {
		return nil, err
	}
	return rec, nil
}

// findSimilarLocked scans same-category memory-domain records for Jaccard similarity.
// Returns the best match above threshold, or nil. Caller must hold s.mu.
func (s *MemoryStore) findSimilarLocked(content, category string, threshold float64) *SearchResult {
	catPrefix := fmt.Appendf(nil, "_idx:cat:%s:", strings.ToLower(category))
	var bestMatch *SearchResult
	bestScore := 0.0

	if err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		for it.Seek(catPrefix); it.ValidForPrefix(catPrefix); it.Next() {
			if vErr := it.Item().Value(func(kVal []byte) error {
				originalKey := string(kVal)
				rec, getErr := loadRecordFromTxn(txn, kVal)
				if getErr == nil && rec.Domain == DomainMemories {
					score := computeJaccard(content, rec.Content)
					if score >= threshold && score > bestScore {
						bestScore = score
						bestMatch = &SearchResult{Key: originalKey, Record: rec}
					}
				}
				return nil
			}); vErr != nil {
				slog.Warn("Failed to read index value during dedup scan", "error", vErr)
			}
		}
		return nil
	}); err != nil {
		slog.Warn("Dedup scan view failed", "category", category, "error", err)
	}

	return bestMatch
}

// mergeTags unions two tag slices, deduplicating by lowercase key.
func mergeTags(existing, incoming []string) []string {
	tagSet := make(map[string]struct{}, len(existing)+len(incoming))
	for _, t := range existing {
		tagSet[strings.ToLower(t)] = struct{}{}
	}
	for _, t := range incoming {
		tagSet[strings.ToLower(t)] = struct{}{}
	}
	merged := make([]string, 0, len(tagSet))
	for t := range tagSet {
		merged = append(merged, t)
	}
	return merged
}

// Get retrieves a Record from the database by key with auto-migration.
func (s *MemoryStore) Get(ctx context.Context, key string) (*Record, error) {
	defer s.RecordPrimitiveLatency("read_graph", time.Now())

	s.mu.RLock()
	defer s.mu.RUnlock()

	var rec *Record
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			var mErr error
			rec, mErr = migrateRecord(val)
			return mErr
		})
	})

	if err != nil {
		if errors.Is(err, badger.ErrKeyNotFound) {
			s.dbMisses.Add(1)
			return nil, fmt.Errorf("memory not found: %s", key)
		}
		return nil, err
	}
	s.dbHits.Add(1)
	return rec, nil
}

// Search matches keys, content, and tags with fuzzy relevance ranking and limits.
//
//nolint:gocognit // Search coordinates multi-phase Badger/index maintenance.
func (s *MemoryStore) Search(ctx context.Context, query string, tagFilter string, limit int) (iter.Seq[*SearchResult], error) {
	defer s.RecordPrimitiveLatency("search_nodes", time.Now())

	s.mu.RLock()
	searchEngine := s.search
	s.mu.RUnlock()

	bleveWasUnavailable := false
	if query != "" && searchEngine != nil {
		var requiredTags []string
		requiredTags = append(requiredTags, "domain:"+DomainMemories)
		if tagFilter != "" {
			requiredTags = append(requiredTags, tagFilter)
		}

		fetchLimit := limit

		hits, err := searchEngine.SearchScoped(ctx, query, nil, requiredTags, fetchLimit)
		if err == nil {
			return func(yield func(*SearchResult) bool) {
				count := 0
				if viewErr := s.db.View(func(txn *badger.Txn) error {
					for _, h := range hits {
						if rec, gErr := s.Get(ctx, h.ID); gErr == nil {
							count++
							s.cacheHits.Add(1)
							if !yield(&SearchResult{Key: h.ID, Record: rec, Score: int(h.Score * 100), Snippets: h.Snippets}) {
								break
							}
							if limit > 0 && count >= limit {
								break
							}
						}
					}
					return nil
				}); viewErr != nil {
					slog.Warn("Memory search bleve hydration view failed", "error", viewErr)
				}
				if count == 0 {
					s.cacheMisses.Add(1)
				}
			}, nil
		}
		slog.Warn("Bleve memory search failed, falling back to badger iteration", "error", err)
		bleveWasUnavailable = true
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return func(yield func(*SearchResult) bool) {
		var candidates []*SearchResult
		tagFilter = strings.ToLower(tagFilter)

		if viewErr := s.db.View(func(txn *badger.Txn) error {
			var scanErr error
			if tagFilter != "" {
				candidates, scanErr = searchByTag(ctx, txn, tagFilter)
			} else {
				candidates, scanErr = searchGeneral(ctx, txn)
			}
			return scanErr
		}); viewErr != nil {
			slog.Warn("Memory search fallback view failed", "error", viewErr)
		}

		var final []*SearchResult

		// If no query, return chronological/original order limited
		if query == "" {
			if limit > 0 && len(candidates) > limit {
				final = candidates[:limit]
			} else {
				final = candidates
			}
		} else {
			// Perform fuzzy matching and scoring
			results := s.rankCandidates(ctx, query, candidates)

			if limit > 0 && len(results) > limit {
				final = results[:limit]
			} else {
				final = results
			}
		}

		if len(final) > 0 {
			s.cacheHits.Add(uint64(len(final)))
			for _, f := range final {
				if !yield(f) {
					break
				}
			}
		} else {
			s.cacheMisses.Add(1)
			if bleveWasUnavailable {
				slog.Warn("Search index unavailable and fallback scan returned empty results", "query", query, "tag_filter", tagFilter)
			}
		}
	}, nil
}

// VacuumSessions performs semantic pruning on sessions matching the target outcome.
// It evicts AST payloads from Bleve (via batch) and writes BadgerDB tombstones.
// Triggers an LSM Flatten if mutates >= flattenThreshold.
// Bounded ValueLog GC is triggered async on exit.
//
//nolint:gocognit // VacuumSessions coordinates multi-phase Badger/index maintenance.
func (s *MemoryStore) VacuumSessions(ctx context.Context, domain string, targetOutcome string, flattenThreshold int, daysOld int) (int, error) {
	defer s.RecordPrimitiveLatency("vacuum_sessions", time.Now())

	s.mu.RLock()
	searchEngine := s.search
	s.mu.RUnlock()

	var targets []string
	var mutated int

	// Find the targeted sessions
	sessionsSeq, err := s.ListSessions(ctx, domain, "", "", targetOutcome, "", 0)
	if err != nil {
		return 0, fmt.Errorf("failed to list sessions for vacuum: %w", err)
	}

	for session := range sessionsSeq {
		if daysOld > 0 && time.Since(session.Record.UpdatedAt) < time.Duration(daysOld)*24*time.Hour {
			continue
		}
		targets = append(targets, session.Key)
	}

	if len(targets) == 0 {
		return 0, nil
	}

	now := time.Now()

	// Fix #4: Dual-threshold chunking (100 keys OR 8MB cumulative bytes)
	// prevents ErrTxnTooBig for large dialectic_history records.
	const maxChunkKeys = 100
	const maxChunkBytes = 8 * 1024 * 1024 // 8MB = 80% of BadgerDB v4's 10MB maxBatchSize

	for chunkStart := 0; chunkStart < len(targets); {
		chunkEnd := chunkStart
		var cumulativeBytes int

		// Determine chunk boundary using dual-threshold
		for chunkEnd < len(targets) && (chunkEnd-chunkStart) < maxChunkKeys && cumulativeBytes < maxChunkBytes {
			chunkEnd++
			// Estimate bytes conservatively; actual tracking happens inside the transaction
			cumulativeBytes += 1024 + 200 // 1KB estimated record + 200B index overhead
		}
		chunk := targets[chunkStart:chunkEnd]

		err = s.UpdateWithRetry(func(txn *badger.Txn) error {
			for _, key := range chunk {
				item, err := txn.Get([]byte(key))
				if err != nil {
					continue
				}
				err = item.Value(func(val []byte) error {
					if rec, err := migrateRecord(val); err == nil {
						// Prepare Tombstone
						rec.Content = fmt.Sprintf(`{"status": "tombstoned", "original_outcome": %q, "vacuumed_at": %q, "reason": "semantic pruning"}`, targetOutcome, now.Format(time.RFC3339))
						rec.UpdatedAt = now

						// Strip original outcome tags to prevent recursive vacuuming
						var newTags []string
						for _, t := range rec.Tags {
							if !strings.HasPrefix(strings.ToLower(t), "outcome:") {
								newTags = append(newTags, t)
							}
						}
						newTags = append(newTags, "outcome:tombstoned")
						rec.Tags = newTags

						data, err := marshalRecord(rec)
						if err != nil {
							return err
						}

						entry := badger.NewEntry([]byte(key), data)
						if err := txn.SetEntry(entry); err != nil {
							return err
						}

						mutated++
					}
					return nil
				})
				if err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			slog.Error("VacuumSessions chunk failed", "chunkStart", chunkStart, "chunkEnd", chunkEnd, "error", err)
			break
		}
		chunkStart = chunkEnd
	}

	if err != nil {
		return mutated, fmt.Errorf("badger update failed: %w", err)
	}

	// Process Bleve individual deletes since we don't have a BatchDelete on the custom SearchEngine interface yet
	if searchEngine != nil {
		for _, key := range targets {
			if delErr := searchEngine.Delete(key); delErr != nil {
				slog.Warn("VacuumSessions search index delete failed (non-fatal)", "key", key, "error", delErr)
			}
		}
	}

	// Trigger Flatten
	if flattenThreshold > 0 && mutated >= flattenThreshold {
		slog.Warn("LSM Flatten triggered by context vacuum threshold", "mutated", mutated, "threshold", flattenThreshold)
		if flatErr := s.db.Flatten(1); flatErr != nil {
			slog.Warn("VacuumSessions Flatten failed", "error", flatErr)
		}
	}

	// Trigger Async ValueLog GC — deferred to execute ONCE after all chunks complete
	go func() {
		reclaimed := 0
		for range 100 { // Bounded safety loop
			gcErr := s.db.RunValueLogGC(0.5)
			if gcErr != nil {
				break
			}
			reclaimed++
		}
		if reclaimed > 0 {
			slog.Info("Context vacuum GC reclaimed disk blocks", "blocks_rewritten", reclaimed)
		}
	}()

	return mutated, nil
}

// ---------------------------------------------------------------------------
// Universal Vacuum Types
// ---------------------------------------------------------------------------

// StaleEntry represents a memory entry flagged as stale during vacuum analysis.
type StaleEntry struct {
	Key       string `json:"key"`
	Category  string `json:"category"`
	AgeDays   int    `json:"age_days"`
	UpdatedAt string `json:"updated_at"`
}

// DuplicateCluster groups keys that are near-duplicates by content similarity.
type DuplicateCluster struct {
	Category string   `json:"category"`
	Keys     []string `json:"keys"`
	Score    float64  `json:"similarity_score"`
}

// CategoryHealth summarizes the health of a single category.
type CategoryHealth struct {
	Category    string `json:"category"`
	EntryCount  int    `json:"entry_count"`
	AvgAgeDays  int    `json:"avg_age_days"`
	StalestDays int    `json:"stalest_days"`
}

// VacuumReport is the unified result type for all vacuum operations across namespaces.
type VacuumReport struct {
	Namespace          string             `json:"namespace"`
	TotalScanned       int                `json:"total_scanned"`
	StaleEntries       []StaleEntry       `json:"stale_entries,omitempty,omitzero"`
	DuplicateClusters  []DuplicateCluster `json:"duplicate_clusters,omitempty,omitzero"`
	CategoryHealthList []CategoryHealth   `json:"category_health,omitempty,omitzero"`
	Pruned             int                `json:"pruned"`
	Merged             int                `json:"merged"`
	ReportOnly         bool               `json:"report_only"`
}

// ---------------------------------------------------------------------------
// VacuumMemories: Staleness + Duplicate Detection for the memories namespace
// ---------------------------------------------------------------------------

// VacuumMemories scans the memories domain for stale entries and near-duplicates.
// When reportOnly is true, analysis is returned without mutations.
// The Jaccard dedup scan is capped at 100 entries per category to avoid O(n²) blowup.
//
//nolint:gocognit // VacuumMemories coordinates multi-phase Badger/index maintenance.
func (s *MemoryStore) VacuumMemories(ctx context.Context, dedupThreshold float64, categoryFilter string, reportOnly bool) (*VacuumReport, error) {
	s.mu.RLock()
	searchEngine := s.search
	s.mu.RUnlock()

	now := time.Now()
	report := &VacuumReport{
		Namespace:  DomainMemories,
		ReportOnly: reportOnly,
	}

	// Phase 1: Collect all memory-domain records grouped by category.
	type memEntry struct {
		key     string
		rec     *Record
		ageDays int
	}
	byCategory := make(map[string][]memEntry)

	if err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		prefix := []byte("_idx:domain:memories:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			if err := it.Item().Value(func(kVal []byte) error {
				originalKey := string(kVal)
				rec, getErr := loadRecordFromTxn(txn, kVal)
				if getErr == nil && rec.Domain == DomainMemories {
					if categoryFilter == "" || rec.Category == categoryFilter {
						age := int(now.Sub(rec.UpdatedAt).Hours() / 24)
						report.TotalScanned++
						byCategory[rec.Category] = append(byCategory[rec.Category], memEntry{
							key:     originalKey,
							rec:     rec,
							ageDays: age,
						})
					}
				}
				return nil
			}); err != nil {
				slog.Warn("Error scanning memory during vacuum", "error", err)
			}
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("memory vacuum scan failed: %w", err)
	}

	// Phase 2: Analyze each category.
	var mergeKeys []string

	categories := make([]string, 0, len(byCategory))
	for cat := range byCategory {
		categories = append(categories, cat)
	}
	sort.Strings(categories)

	for _, cat := range categories {
		entries := byCategory[cat]
		totalAge := 0
		stalest := 0

		for _, e := range entries {
			totalAge += e.ageDays
			if e.ageDays > stalest {
				stalest = e.ageDays
			}
		}

		avgAge := 0
		if len(entries) > 0 {
			avgAge = totalAge / len(entries)
		}
		report.CategoryHealthList = append(report.CategoryHealthList, CategoryHealth{
			Category:    cat,
			EntryCount:  len(entries),
			AvgAgeDays:  avgAge,
			StalestDays: stalest,
		})

		// Jaccard duplicate detection — bounded to 100 entries per category.
		sampleSize := min(len(entries), 100)
		sample := entries[:sampleSize]

		// Track which keys are already part of a cluster to avoid duplicating.
		clustered := make(map[string]bool)
		for i := range sample {
			if clustered[sample[i].key] {
				continue
			}
			var cluster []string
			var bestScore float64
			for j := i + 1; j < len(sample); j++ {
				if clustered[sample[j].key] {
					continue
				}
				score := computeJaccard(sample[i].rec.Content, sample[j].rec.Content)
				if score >= dedupThreshold {
					if len(cluster) == 0 {
						cluster = append(cluster, sample[i].key)
						clustered[sample[i].key] = true
					}
					cluster = append(cluster, sample[j].key)
					clustered[sample[j].key] = true
					if score > bestScore {
						bestScore = score
					}
				}
			}
			if len(cluster) > 1 {
				report.DuplicateClusters = append(report.DuplicateClusters, DuplicateCluster{
					Category: cat,
					Keys:     cluster,
					Score:    bestScore,
				})
				// For merge: keep the first key (oldest), mark rest for removal.
				mergeKeys = append(mergeKeys, cluster[1:]...)
			}
		}
	}

	// Phase 3: Mutate if not report-only.
	if !reportOnly {
		var allPrune []string
		allPrune = append(allPrune, mergeKeys...)

		if len(allPrune) > 0 {
			// Deduplicate keys
			seen := make(map[string]bool, len(allPrune))
			unique := allPrune[:0]
			for _, k := range allPrune {
				if !seen[k] {
					seen[k] = true
					unique = append(unique, k)
				}
			}
			allPrune = unique

			if err := s.DeleteBatch(ctx, allPrune); err != nil {
				return report, fmt.Errorf("vacuum memory pruning failed: %w", err)
			}
		}

		report.Merged = len(mergeKeys)

		s.triggerDBMaintenance(len(allPrune), 1000)
	}

	// Remove Bleve refs for stale entries even in report_only to keep index tidy?
	// No — report_only means NO mutations at all.
	_ = searchEngine // Defensive: used only when !reportOnly via DeleteBatch path.

	return report, nil
}

// ---------------------------------------------------------------------------
// VacuumStandards: Orphan Detection for the standards namespace
// ---------------------------------------------------------------------------

// VacuumStandards scans the standards domain for orphaned drift checksums
// (SysDrift keys with no corresponding symbol records) and empty packages.
// When reportOnly is true, analysis is returned without mutations.
func (s *MemoryStore) VacuumStandards(ctx context.Context, reportOnly bool) (*VacuumReport, error) {
	report := &VacuumReport{
		Namespace:  DomainStandards,
		ReportOnly: reportOnly,
	}

	// Collect all standards keys grouped by package.
	type pkgStats struct {
		symbolKeys []string
		driftKey   string
	}
	packages := make(map[string]*pkgStats)

	if err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		prefix := []byte("pkg:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			key := string(it.Item().Key())
			if err := it.Item().Value(func(v []byte) error {
				rec, mErr := migrateRecord(v)
				if mErr == nil {
					report.TotalScanned++

					// Extract package path from key: "pkg:<path>:<SymbolName>"
					parts := strings.SplitN(strings.TrimPrefix(key, "pkg:"), ":", 2)
					if len(parts) >= 2 {
						pkgPath := parts[0]

						if _, ok := packages[pkgPath]; !ok {
							packages[pkgPath] = &pkgStats{}
						}

						if rec.Category == catSysDrift {
							packages[pkgPath].driftKey = key
						} else {
							packages[pkgPath].symbolKeys = append(packages[pkgPath].symbolKeys, key)
						}
					}
				}
				return nil
			}); err != nil {
				slog.Warn("Error scanning standards during vacuum", "error", err)
			}
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("standards vacuum scan failed: %w", err)
	}

	// Find orphans: drift keys with no symbols, or empty packages.
	var orphanKeys []string
	for pkgPath, stats := range packages {
		if stats.driftKey != "" && len(stats.symbolKeys) == 0 {
			report.StaleEntries = append(report.StaleEntries, StaleEntry{
				Key:      stats.driftKey,
				Category: "SysDrift (orphaned)",
			})
			orphanKeys = append(orphanKeys, stats.driftKey)
		}
		_ = pkgPath // used in the map iteration
	}

	if !reportOnly && len(orphanKeys) > 0 {
		if err := s.DeleteBatch(ctx, orphanKeys); err != nil {
			return report, fmt.Errorf("vacuum standards orphan cleanup failed: %w", err)
		}
		report.Pruned = len(orphanKeys)
		s.triggerDBMaintenance(len(orphanKeys), 1000)
	}

	return report, nil
}

// triggerDBMaintenance performs LSM Flatten + async ValueLog GC if mutations exceed the threshold.
// Extracted from VacuumSessions to share across all vacuum namespaces.
func (s *MemoryStore) triggerDBMaintenance(mutated, flattenThreshold int) {
	s.gcPrunedNodes.Add(uint64(mutated)) //nolint:gosec // mutated count is bounded by batch size

	if mutated >= flattenThreshold {
		slog.Warn("LSM Flatten triggered by context vacuum threshold", "mutated", mutated, "threshold", flattenThreshold)
		if flatErr := s.db.Flatten(1); flatErr != nil {
			slog.Warn("triggerDBMaintenance Flatten failed", "error", flatErr)
		}
	}

	go func() {
		reclaimed := 0
		for range 100 {
			gcErr := s.db.RunValueLogGC(0.5)
			if gcErr != nil {
				break
			}
			reclaimed++
		}
		if reclaimed > 0 {
			s.gcSweeps.Add(uint64(reclaimed))
			slog.Info("Context vacuum GC reclaimed disk blocks", "blocks_rewritten", reclaimed)
		}
	}()
}

// searchByTag performs an O(K) index-based scan for records with a specific tag.
func searchByTag(ctx context.Context, txn *badger.Txn, tagFilter string) ([]*SearchResult, error) {
	var candidates []*SearchResult
	it := txn.NewIterator(badger.DefaultIteratorOptions)
	defer it.Close()

	prefix := fmt.Appendf(nil, "_idx:tag:%s:", tagFilter)
	for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if err := it.Item().Value(func(kVal []byte) error {
			originalKey := string(kVal)
			rec, getErr := loadRecordFromTxn(txn, kVal)
			if getErr == nil && !HarvestedCategories[rec.Category] {
				candidates = append(candidates, &SearchResult{Key: originalKey, Record: rec})
			}
			return nil
		}); err != nil {
			slog.Warn("Corrupted memory entry detected during search (tag)", "error", err)
		}
	}
	return candidates, nil
}

// searchGeneral performs a linear scan of all non-index records.
func searchGeneral(ctx context.Context, txn *badger.Txn) ([]*SearchResult, error) {
	var candidates []*SearchResult

	// Use domain index prefix scan instead of full table scan.
	// Previously iterated ALL keys and filtered by HarvestedCategories;
	// now targets only the memories domain via the secondary index.
	prefix := []byte("_idx:domain:" + DomainMemories + ":")
	opts := badger.DefaultIteratorOptions
	opts.PrefetchValues = true
	it := txn.NewIterator(opts)
	defer it.Close()

	for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if err := it.Item().Value(func(kVal []byte) error {
			actualKey := string(kVal)
			rec, getErr := loadRecordFromTxn(txn, kVal)
			if getErr == nil {
				candidates = append(candidates, &SearchResult{Key: actualKey, Record: rec})
			}
			return nil
		}); err != nil {
			slog.Warn("Corrupted memory entry detected during search (general)", "error", err)
		}
	}
	return candidates, nil
}

// rankCandidates applies fuzzy scoring across Key, Content, and Tags with parallelism.
func (s *MemoryStore) rankCandidates(ctx context.Context, query string, candidates []*SearchResult) []*SearchResult {
	if len(candidates) == 0 {
		return candidates
	}

	var wg sync.WaitGroup
	// Concurrency: Use worker pool based on CPU count or chunking
	workerCount := min(len(candidates), 4)

	chunkSize := (len(candidates) + workerCount - 1) / workerCount
	for i := range workerCount {
		start := i * chunkSize
		if start >= len(candidates) {
			break
		}
		end := min(start+chunkSize, len(candidates))

		wg.Add(1)
		go func(ctx context.Context, subset []*SearchResult) {
			defer wg.Done()
			for _, c := range subset {
				select {
				case <-ctx.Done():
					return
				default:
					scoreSingleCandidate(query, c)
				}
			}
		}(ctx, candidates[start:end])
	}
	wg.Wait()

	// Filter out zero scores and sort
	var ranked []*SearchResult
	for _, c := range candidates {
		if c.Score > 0 {
			ranked = append(ranked, c)
		}
	}

	slices.SortFunc(ranked, func(a, b *SearchResult) int {
		if a.Score < b.Score {
			return 1
		}
		if a.Score > b.Score {
			return -1
		}
		return 0
	})

	return ranked
}

// scoreSingleCandidate scores a candidate against the query across key, content, category, and tags.
func scoreSingleCandidate(query string, c *SearchResult) {
	if c.Record == nil {
		return
	}
	// 1. Score the Key
	keyMatches := fuzzy.Find(query, []string{c.Key})
	if len(keyMatches) > 0 {
		c.Score = keyMatches[0].Score
	}

	// 2. Score Content (if better)
	contentMatches := fuzzy.Find(query, []string{c.Record.Content})
	if len(contentMatches) > 0 && contentMatches[0].Score > c.Score {
		c.Score = contentMatches[0].Score
	}

	// 3. Score Category
	if c.Record.Category != "" {
		catMatches := fuzzy.Find(query, []string{c.Record.Category})
		if len(catMatches) > 0 && catMatches[0].Score > c.Score {
			c.Score = catMatches[0].Score
		}
	}

	// 4. Score Tags (if better)
	for _, t := range c.Record.Tags {
		tagMatches := fuzzy.Find(query, []string{t})
		if len(tagMatches) > 0 && tagMatches[0].Score > c.Score {
			c.Score = tagMatches[0].Score
		}
	}
}

// GetRecent retrieves the last N memories sorted by UpdatedAt descending.
func (s *MemoryStore) GetRecent(ctx context.Context, count int) ([]*SearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*SearchResult
	if count <= 0 {
		return results, nil
	}

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Reverse = true // Latest first

		it := txn.NewIterator(opts)
		defer it.Close()

		prefix := []byte("_idx:t:")
		seekKey := append([]byte(nil), prefix...)
		seekKey = append(seekKey, 0xff, 0xff, 0xff)

		for it.Seek(seekKey); it.ValidForPrefix(prefix) && len(results) < count; it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				originalKey := val
				rec, getErr := loadRecordFromTxn(txn, originalKey)
				if getErr == nil && rec.Domain == DomainMemories {
					results = append(results, &SearchResult{
						Key:    string(originalKey),
						Record: rec,
					})
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	if err == nil {
		if len(results) > 0 {
			s.dbHits.Add(uint64(len(results)))
		} else {
			s.dbMisses.Add(1)
		}
	}
	return results, err
}

// ListKeys retrieves all available keys for knowledge discovery.
// Scoped exclusively to the memories domain via the _idx:domain:memories: index.
func (s *MemoryStore) ListKeys(ctx context.Context) (iter.Seq[*SearchResult], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return func(yield func(*SearchResult) bool) {
		if viewErr := s.db.View(func(txn *badger.Txn) error {
			it := txn.NewIterator(badger.DefaultIteratorOptions)
			defer it.Close()

			prefix := []byte("_idx:domain:memories:")
			count := 0
			stop := false
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				if stop {
					break
				}
				select {
				case <-ctx.Done():
					return nil
				default:
				}
				if err := it.Item().Value(func(kVal []byte) error {
					originalKey := string(kVal)
					rec, getErr := loadRecordFromTxn(txn, kVal)
					if getErr == nil {
						count++
						s.dbHits.Add(1)
						if !yield(&SearchResult{Key: originalKey, Record: rec}) {
							stop = true
						}
					}
					return nil
				}); err != nil {
					slog.Warn("Corrupted memory entry detected during list", "error", err)
				}
			}
			if count == 0 {
				s.dbMisses.Add(1)
			}
			return nil
		}); viewErr != nil {
			slog.Warn("ListKeys view failed", "error", viewErr)
		}
	}, nil
}

// FindSessionBySuffix scans the domain index for the most recent session
// whose key ends with the given suffix (e.g., ":session_id"). Uses a targeted
// prefix scan on the domain index instead of materializing all sessions.
func (s *MemoryStore) FindSessionBySuffix(ctx context.Context, domain, suffix string) (*SearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if domain == "" {
		domain = DomainSessions
	}

	var bestMatch *SearchResult

	err := s.db.View(func(txn *badger.Txn) error {
		prefix := []byte("_idx:domain:" + domain + ":")
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			if err := it.Item().Value(func(kVal []byte) error {
				actualKey := string(kVal)
				if !strings.HasSuffix(actualKey, suffix) {
					return nil
				}

				rec, getErr := loadRecordFromTxn(txn, kVal)
				if getErr == nil && rec.Domain == domain {
					if bestMatch == nil || rec.UpdatedAt.After(bestMatch.Record.UpdatedAt) {
						bestMatch = &SearchResult{Key: actualKey, Record: rec}
					}
				}
				return nil
			}); err != nil {
				slog.Warn("Error during session suffix scan", "domain", domain, "error", err)
			}
		}
		return nil
	})

	return bestMatch, err
}

// ListSessions performs the ListSessions operation.
//
//nolint:gocognit // ListSessions coordinates multi-phase Badger/index maintenance.
func (s *MemoryStore) ListSessions(ctx context.Context, domain, projectID, serverID, outcome, traceContext string, limit int) (iter.Seq[*SearchResult], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if domain == "" {
		domain = DomainSessions
	}

	return func(yield func(*SearchResult) bool) {
		if viewErr := s.db.View(func(txn *badger.Txn) error {
			// Default to domain index, narrow dynamically if tags specify tighter bounds
			prefixStr := fmt.Sprintf("_idx:domain:%s:", domain)
			if traceContext != "" {
				prefixStr = fmt.Sprintf("_idx:tag:trace:%s:", strings.ToLower(traceContext))
			} else if projectID != "" {
				prefixStr = fmt.Sprintf("_idx:tag:project:%s:", strings.ToLower(projectID))
			} else if outcome != "" {
				prefixStr = fmt.Sprintf("_idx:tag:outcome:%s:", strings.ToLower(outcome))
			}

			opts := badger.DefaultIteratorOptions
			opts.Prefix = []byte(prefixStr)
			opts.PrefetchValues = true // We need the actual key value from the index
			it := txn.NewIterator(opts)
			defer it.Close()

			count := 0
			stop := false
			for it.Rewind(); it.Valid(); it.Next() {
				if stop {
					break
				}
				select {
				case <-ctx.Done():
					return nil
				default:
				}
				idxItem := it.Item()
				actualKey, err := idxItem.ValueCopy(nil)
				if err != nil {
					continue
				}

				// Validate server filter physically on the exact target key bounding
				if serverID != "" && !strings.HasPrefix(string(actualKey), serverID+":") {
					continue
				}

				recItem, err := txn.Get(actualKey)
				if err != nil {
					continue // index orphaned?
				}

				if err := recItem.Value(func(v []byte) error {
					if rec, err := migrateRecord(v); err == nil && rec.Domain == domain {
						// Client-side cross-filtering to verify secondary filters not caught by the primary prefix scan
						if s.matchSessionFilters(rec, projectID, outcome, traceContext) {
							count++
							s.dbHits.Add(1)
							if !yield(&SearchResult{Key: string(actualKey), Record: rec}) {
								stop = true
							}
						}
					}
					return nil
				}); err != nil {
					slog.Warn("Corrupted session entry detected during list", "error", err)
				}

				// Safety cap: prevent unbounded memory allocation or respect user limit
				if limit > 0 && count >= limit {
					break
				} else if limit <= 0 && count >= 500 { // fallback safety cap
					break
				}
			}
			if count == 0 {
				s.dbMisses.Add(1)
			}
			return nil
		}); viewErr != nil {
			slog.Warn("ListSessions view failed", "error", viewErr)
		}
	}, nil
}

// SearchSessions performs the SearchSessions operation.
//
//nolint:gocognit // SearchSessions coordinates multi-phase Badger/index maintenance.
func (s *MemoryStore) SearchSessions(ctx context.Context, domain, query, projectID, serverID, outcome, traceContext string, limit int) (iter.Seq[*SearchResult], error) {
	s.mu.RLock()
	searchEngine := s.search
	s.mu.RUnlock()

	if domain == "" {
		domain = DomainSessions
	}

	if query != "" && searchEngine != nil {
		var requiredTags []string
		requiredTags = append(requiredTags, "domain:"+domain)
		if projectID != "" {
			requiredTags = append(requiredTags, "project:"+strings.ToLower(projectID))
		}
		if outcome != "" {
			requiredTags = append(requiredTags, "outcome:"+strings.ToLower(outcome))
		}
		if traceContext != "" {
			requiredTags = append(requiredTags, "trace:"+strings.ToLower(traceContext))
		}

		var categories []string
		if serverID != "" {
			categories = append(categories, serverID)
		}

		fetchLimit := limit

		hits, err := searchEngine.SearchScoped(ctx, query, categories, requiredTags, fetchLimit)
		if err == nil {
			return func(yield func(*SearchResult) bool) {
				count := 0
				if viewErr := s.db.View(func(txn *badger.Txn) error {
					for _, h := range hits {
						if rec, gErr := s.Get(ctx, h.ID); gErr == nil {
							count++
							s.cacheHits.Add(1)
							if !yield(&SearchResult{Key: h.ID, Record: rec, Score: int(h.Score * 100), Snippets: h.Snippets}) {
								break
							}
							if limit > 0 && count >= limit {
								break
							}
						}
					}
					return nil
				}); viewErr != nil {
					slog.Warn("Session search bleve hydration view failed", "error", viewErr)
				}
				if count == 0 {
					s.cacheMisses.Add(1)
				}
			}, nil
		}
		slog.Warn("Bleve session search failed, falling back to list scan", "error", err)
	}

	candidatesSeq, err := s.ListSessions(ctx, domain, projectID, serverID, outcome, traceContext, limit)
	if err != nil {
		return nil, err
	}

	return func(yield func(*SearchResult) bool) {
		var candidates []*SearchResult
		for c := range candidatesSeq {
			candidates = append(candidates, c)
		}

		var final []*SearchResult
		if query == "" {
			final = candidates
		} else {
			final = s.rankCandidates(ctx, query, candidates)
		}

		if limit > 0 && len(final) > limit {
			final = final[:limit]
		}

		if len(final) > 0 {
			s.cacheHits.Add(uint64(len(final)))
			for _, f := range final {
				if !yield(f) {
					break
				}
			}
		} else {
			s.cacheMisses.Add(1)
		}
	}, nil
}

func (s *MemoryStore) matchSessionFilters(rec *Record, pID, out, trace string) bool {
	tags := make(map[string]bool)
	for _, t := range rec.Tags {
		tags[strings.ToLower(t)] = true
	}
	if pID != "" && !tags[fmt.Sprintf("project:%s", strings.ToLower(pID))] {
		return false
	}
	if out != "" && !tags[fmt.Sprintf("outcome:%s", strings.ToLower(out))] {
		return false
	}
	if trace != "" && !tags[fmt.Sprintf("trace:%s", strings.ToLower(trace))] {
		return false
	}
	return true
}

// Clear removes all stored memories by dropping and recreating or clearing.
func (s *MemoryStore) Clear(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.db.DropAll(); err != nil {
		return err
	}
	for _, counter := range s.namespaceCounts {
		counter.Store(0)
	}

	// Reset Bleve index to empty.
	if s.search != nil {
		if sErr := s.search.Rebuild(ctx, nil); sErr != nil {
			slog.Warn("Bleve index reset failed after clear (non-fatal)", "error", sErr)
		}
	}

	return nil
}

// GetMetrics returns a snapshot of cache performance.
func (s *MemoryStore) GetMetrics() CacheMetrics {
	var metrics CacheMetrics
	metrics.Hits = s.cacheHits.Load()
	metrics.Misses = s.cacheMisses.Load()
	metrics.DBHits = s.dbHits.Load()
	metrics.DBMisses = s.dbMisses.Load()
	metrics.Namespaces = make(map[string]int, len(s.namespaceCounts))
	for domain, counter := range s.namespaceCounts {
		metrics.Namespaces[domain] = int(counter.Load())
	}

	if count, err := s.DocCount(); err == nil {
		metrics.BleveDocs = count
	}

	var statsErr error
	if metrics.Entries, _, statsErr = s.GetStats(); statsErr != nil {
		slog.Debug("Failed to get db stats for metrics", "error", statsErr)
	}
	return metrics
}

// GetStats returns usage statistics about the memory store.
// Uses pre-maintained atomic counters instead of a full DB scan for O(1) count.
func (s *MemoryStore) GetStats() (int, int64, error) {
	var count int64
	for _, counter := range s.namespaceCounts {
		count += counter.Load()
	}

	lsm, vlog := s.db.Size()
	return int(count), lsm + vlog, nil
}

// Delete removes a specific memory and its secondary index.
// Scoped to the memories domain — rejects keys belonging to the standards namespace.
func (s *MemoryStore) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Pre-flight: reject standards-domain records.
	var rec *Record
	if err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			r, mErr := migrateRecord(val)
			if mErr != nil {
				slog.Warn("Failed to migrate record during delete pre-flight", "key", key, "error", mErr)
				return nil
			}
			rec = r
			return nil
		})
	}); err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
		slog.Warn("Delete pre-flight view failed", "key", key, "error", err)
	}
	if rec != nil && rec.Domain == DomainStandards {
		return fmt.Errorf("key %q belongs to the standards domain; use standards tools to manage it", key)
	}

	if err := s.deleteNoLock(key); err != nil {
		return err
	}

	// Write-through: remove from Bleve index (best-effort).
	if s.search != nil {
		if sErr := s.search.Delete(key); sErr != nil {
			slog.Warn("Bleve index delete failed (non-fatal)", "key", key, "error", sErr)
		}
	}

	return nil
}

// BatchDelete removes multiple records simultaneously using a single write transaction.
func (s *MemoryStore) BatchDelete(ctx context.Context, keys []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	recs := make(map[string]*Record)
	if err := s.db.View(func(txn *badger.Txn) error {
		for _, key := range keys {
			item, err := txn.Get([]byte(key))
			if err != nil {
				continue
			}
			if err := item.Value(func(val []byte) error {
				if r, mErr := migrateRecord(val); mErr == nil {
					recs[key] = r
				}
				return nil
			}); err != nil {
				slog.Warn("BatchDelete pre-flight value read failed", "key", key, "error", err)
			}
		}
		return nil
	}); err != nil {
		slog.Warn("BatchDelete pre-flight view failed", "error", err)
	}

	for key, rec := range recs {
		if rec.Domain == DomainStandards {
			return fmt.Errorf("key %q belongs to the standards domain; use standards tools to manage it", key)
		}
	}

	err := s.UpdateWithRetry(func(txn *badger.Txn) error {
		for _, key := range keys {
			if rec, ok := recs[key]; ok {
				s.deleteRecordIndices(txn, key, rec)
				if counter, exists := s.namespaceCounts[rec.Domain]; exists {
					if counter.Add(-1) == 0 && rec.Domain != DomainMemories {
						select {
						case s.namespaceEvents <- rec.Domain:
						default:
						}
					}
				}
			}
			if err := txn.Delete([]byte(key)); err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	if s.search != nil {
		for _, key := range keys {
			if sErr := s.search.Delete(key); sErr != nil {
				slog.Warn("Bleve index delete failed in batch (non-fatal)", "key", key, "error", sErr)
			}
		}
	}
	return nil
}

func (s *MemoryStore) deleteNoLock(key string) error {
	var rec *Record
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			var mErr error
			rec, mErr = migrateRecord(val)
			return mErr
		})
	})
	if err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
		slog.Error("Failed to fetch record for deletion", "key", key, "error", err)
	}

	return s.UpdateWithRetry(func(txn *badger.Txn) error {
		if rec != nil {
			s.deleteRecordIndices(txn, key, rec)
			if counter, exists := s.namespaceCounts[rec.Domain]; exists {
				if counter.Add(-1) == 0 && rec.Domain != DomainMemories {
					select {
					case s.namespaceEvents <- rec.Domain:
					default:
					}
				}
			}
		}
		return txn.Delete([]byte(key))
	})
}

func (s *MemoryStore) deleteRecordIndices(txn *badger.Txn, key string, rec *Record) {
	// 1. Time Index
	timeIdx := fmt.Sprintf("_idx:t:%x:%s", rec.UpdatedAt.UnixNano(), key)
	if err := txn.Delete([]byte(timeIdx)); err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
		slog.Warn("Failed to delete time index", "key", key, "error", err)
	}

	// 2. Tag Indices
	for _, t := range rec.Tags {
		tagIdx := fmt.Sprintf("_idx:tag:%s:%s", strings.ToLower(t), key)
		if err := txn.Delete([]byte(tagIdx)); err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
			slog.Warn("Failed to delete tag index", "tag", t, "key", key, "error", err)
		}
	}

	// 3. Category Index
	if rec.Category != "" {
		catIdx := fmt.Sprintf("_idx:cat:%s:%s", strings.ToLower(rec.Category), key)
		if err := txn.Delete([]byte(catIdx)); err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
			slog.Warn("Failed to delete category index", "cat", rec.Category, "key", key, "error", err)
		}
	}

	// 4. Domain Index
	if rec.Domain != "" {
		domIdx := fmt.Sprintf("_idx:domain:%s:%s", rec.Domain, key)
		if err := txn.Delete([]byte(domIdx)); err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
			slog.Warn("Failed to delete domain index", "domain", rec.Domain, "key", key, "error", err)
		}
	}
}

func (s *MemoryStore) createRecordIndices(txn *badger.Txn, key string, rec *Record) error {
	// 1. Time Index
	timeIdx := fmt.Sprintf("_idx:t:%x:%s", rec.UpdatedAt.UnixNano(), key)
	if err := txn.Set([]byte(timeIdx), []byte(key)); err != nil {
		return fmt.Errorf("failed to set time index: %w", err)
	}

	// 2. Category Index
	if rec.Category != "" {
		catIdx := fmt.Sprintf("_idx:cat:%s:%s", strings.ToLower(rec.Category), key)
		if err := txn.Set([]byte(catIdx), []byte(key)); err != nil {
			return fmt.Errorf("failed to set category index: %w", err)
		}
	}

	// 3. Tag Indices
	for _, t := range rec.Tags {
		tagIdx := fmt.Sprintf("_idx:tag:%s:%s", strings.ToLower(t), key)
		if err := txn.Set([]byte(tagIdx), []byte(key)); err != nil {
			return fmt.Errorf("failed to set tag index for %s: %w", t, err)
		}
	}

	// 4. Domain Index
	if rec.Domain != "" {
		domIdx := fmt.Sprintf("_idx:domain:%s:%s", rec.Domain, key)
		if err := txn.Set([]byte(domIdx), []byte(key)); err != nil {
			return fmt.Errorf("failed to set domain index: %w", err)
		}
	}

	return nil
}

// BatchEntry represents a single item in a batch write operation.
type BatchEntry struct {
	Title      string    `json:"title,omitempty,omitzero"`
	Key        string    `json:"key"`
	Value      string    `json:"value"`
	Category   string    `json:"category,omitempty,omitzero"`
	Domain     string    `json:"domain,omitempty,omitzero"`
	SessionID  string    `json:"session_id,omitempty,omitzero"`
	Tags       []string  `json:"tags,omitempty,omitzero"`
	SourcePath string    `json:"source_path,omitempty,omitzero"`
	SourceHash string    `json:"source_hash,omitempty,omitzero"`
	SymbolName string    `json:"symbolname,omitempty,omitzero"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// BatchError reports a per-key failure during batch operations.
type BatchError struct {
	Key   string `json:"key"`
	Error string `json:"error"`
}

// SaveBatch atomically stores multiple entries in a single BadgerDB transaction.
// All entries are committed together; if any fails, the entire batch is rolled back.
func (s *MemoryStore) SaveBatch(ctx context.Context, entries []BatchEntry) (stored int, batchErrors []BatchError, err error) {
	if len(entries) == 0 {
		return 0, nil, nil
	}
	if len(entries) > s.maxBatchSize {
		return 0, nil, fmt.Errorf("batch size %d exceeds maximum of %d", len(entries), s.maxBatchSize)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Phase 1: Collect existing records for index cleanup (read-only pass).
	oldRecords, lookupErr := s.collectExistingRecords(entries)
	if lookupErr != nil {
		return 0, nil, lookupErr
	}

	// Phase 2: Atomic write — commit all entries + indices in a single transaction.
	now := time.Now()
	err = s.UpdateWithRetry(func(txn *badger.Txn) error {
		for _, e := range entries {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			// Clean up old indices if updating an existing key.
			if oldRec, exists := oldRecords[e.Key]; exists {
				s.deleteRecordIndices(txn, e.Key, oldRec)
			}

			rec := &Record{
				Title:      e.Title,
				SymbolName: e.SymbolName,
				Content:    e.Value,
				Category:   e.Category,
				Tags:       e.Tags,
				SourcePath: e.SourcePath,
				SourceHash: e.SourceHash,
			}

			// Temporal fidelity: honor imported timestamps when present,
			// otherwise default to current time.
			if !e.UpdatedAt.IsZero() {
				rec.UpdatedAt = e.UpdatedAt
			} else {
				rec.UpdatedAt = now
			}

			if e.Domain != "" {
				rec.Domain = e.Domain
			} else {
				if HarvestedCategories[e.Category] {
					rec.Domain = DomainStandards
				} else {
					rec.Domain = DomainMemories
				}
			}

			if e.SessionID != "" {
				rec.SessionID = e.SessionID
			}

			if oldRec, exists := oldRecords[e.Key]; exists {
				rec.CreatedAt = oldRec.CreatedAt
			} else {
				if !e.CreatedAt.IsZero() {
					rec.CreatedAt = e.CreatedAt
				} else {
					rec.CreatedAt = now
				}
				if counter, exists := s.namespaceCounts[rec.Domain]; exists {
					counter.Add(1)
				}
			}

			data, err := marshalRecord(rec)
			if err != nil {
				return fmt.Errorf("failed to marshal record for key %q: %w", e.Key, err)
			}
			if err := txn.Set([]byte(e.Key), data); err != nil {
				return fmt.Errorf("failed to set key %q: %w", e.Key, err)
			}
			if err := s.createRecordIndices(txn, e.Key, rec); err != nil {
				return fmt.Errorf("failed to index key %q: %w", e.Key, err)
			}
		}
		return nil
	})

	if err != nil {
		s.batchErrors.Add(1)
		return 0, nil, fmt.Errorf("batch write failed (atomic rollback): %w", err)
	}

	// Write-through: bulk index via Bleve Batch (best-effort).
	s.syncBatchToSearchIndex(entries)

	s.batchEntriesProcessed.Add(uint64(len(entries)))

	slog.Info("Batch save completed", "entries", len(entries))
	return len(entries), nil, nil
}

// collectExistingRecords performs a read-only lookup for entries that already exist in the DB.
// Returns a map of key -> existing Record for index cleanup during updates.
func (s *MemoryStore) collectExistingRecords(entries []BatchEntry) (map[string]*Record, error) {
	oldRecords := make(map[string]*Record, len(entries))
	if err := s.db.View(func(txn *badger.Txn) error {
		for _, e := range entries {
			item, err := txn.Get([]byte(e.Key))
			if err != nil {
				if errors.Is(err, badger.ErrKeyNotFound) {
					continue
				}
				return err
			}
			if err := item.Value(func(val []byte) error {
				if old, err := migrateRecord(val); err == nil {
					oldRecords[e.Key] = old
				}
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("batch lookup failure: %w", err)
	}
	return oldRecords, nil
}

// syncBatchToSearchIndex pushes batch entries to the Bleve search index (best-effort).
func (s *MemoryStore) syncBatchToSearchIndex(entries []BatchEntry) {
	if s.search == nil {
		return
	}
	sdocs := make(map[string]*search.Document, len(entries))
	for _, e := range entries {
		// Inject synthetic domain tag for SearchScoped conjunction match
		domain := e.Domain
		if domain == "" {
			if HarvestedCategories[e.Category] {
				domain = DomainStandards
			} else {
				domain = DomainMemories
			}
		}
		indexTags := append(slices.Clone(e.Tags), "domain:"+domain)
		if after, ok := strings.CutPrefix(e.Key, "pkg:"); ok {
			parts := strings.SplitN(after, ":", 2)
			if len(parts) >= 1 {
				indexTags = append(indexTags, "package:"+parts[0])
			}
		}
		sdocs[e.Key] = &search.Document{
			Title:      e.Title,
			SymbolName: e.SymbolName,
			Content:    e.Value,
			Category:   e.Category,
			Tags:       indexTags,
			SourcePath: e.SourcePath,
			SourceHash: e.SourceHash,
		}
	}
	if sErr := s.search.IndexBatch(sdocs); sErr != nil {
		slog.Warn("Bleve batch index failed after SaveBatch (non-fatal)", "count", len(entries), "error", sErr)
	}
}

// GetBatch retrieves multiple records by key in a single read-only transaction.
// Returns found records and a list of missing keys separately.
func (s *MemoryStore) GetBatch(ctx context.Context, keys []string) (map[string]*Record, []string, error) {
	if len(keys) == 0 {
		return nil, nil, nil
	}
	if len(keys) > s.maxBatchSize {
		return nil, nil, fmt.Errorf("batch size %d exceeds maximum of %d", len(keys), s.maxBatchSize)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	found := make(map[string]*Record, len(keys))
	var missing []string

	err := s.db.View(func(txn *badger.Txn) error {
		for _, key := range keys {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			item, err := txn.Get([]byte(key))
			if err != nil {
				if errors.Is(err, badger.ErrKeyNotFound) {
					missing = append(missing, key)
					continue
				}
				return fmt.Errorf("failed to get key %q: %w", key, err)
			}

			if err := item.Value(func(val []byte) error {
				rec, mErr := migrateRecord(val)
				if mErr != nil {
					return mErr
				}
				found[key] = rec
				return nil
			}); err != nil {
				slog.Warn("Corrupted entry in batch read, treating as missing", "key", key, "error", err)
				missing = append(missing, key)
			}
		}
		return nil
	})

	if err != nil {
		return nil, nil, fmt.Errorf("batch read failed: %w", err)
	}

	slog.Info("Batch read completed", "found", len(found), "missing", len(missing))
	return found, missing, nil
}

// Close safely shuts down the database and maintenance routines.
func (s *MemoryStore) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.cleanup.Stop()
		close(s.stopGC)
		close(s.stopAudit)

		// Wait for GC/audit goroutines to finish with a configurable timeout.
		// Default 15s is conservative for the orchestrator's typical 30s SIGKILL window.
		timeout := 15 * time.Second
		if v, ok := os.LookupEnv("MCP_RECALL_SHUTDOWN_TIMEOUT_SECS"); ok {
			if secs, parseErr := strconv.Atoi(v); parseErr == nil && secs > 0 {
				timeout = time.Duration(secs) * time.Second
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		done := make(chan struct{})
		go func() { s.wg.Wait(); close(done) }()
		select {
		case <-done:
			slog.Info("GC/audit goroutines exited cleanly")
		case <-ctx.Done():
			slog.Warn("GC goroutines did not exit within timeout, proceeding with forced close",
				"timeout", timeout)
		}

		if s.search != nil {
			if sErr := s.search.Close(); sErr != nil {
				slog.Warn("Failed to close search engine", "error", sErr)
			}
		}
		if s.dbClosed.CompareAndSwap(false, true) {
			err = s.db.Close()
		}
	})
	runtime.KeepAlive(s)
	return err
}

// UpdateWithRetry wraps badger.Update with exponential backoff for Transaction Conflicts.
// Crucial for mitigating concurrent memory pipeline ingest collisions natively.
func (s *MemoryStore) UpdateWithRetry(fn func(txn *badger.Txn) error) error {
	maxRetries := 5
	backoff := 10 * time.Millisecond

	var err error
	for range maxRetries {
		err = s.db.Update(fn)
		if err == nil {
			return nil
		}
		if !errors.Is(err, badger.ErrConflict) {
			return err
		}

		time.Sleep(backoff)
		backoff *= 2
	}
	return fmt.Errorf("transaction conflict after %d retries: %w", maxRetries, err)
}

// ListCategories retrieves a unique list of all memory categories with counts.
// Standards-domain categories (HarvestedCode, PackageDoc, SysDrift) are excluded.
func (s *MemoryStore) ListCategories(ctx context.Context) (map[string]int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	categories := make(map[string]int)
	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		it := txn.NewIterator(opts)
		defer it.Close()

		prefix := []byte("_idx:cat:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); {
			item := it.Item()
			key := string(item.Key())
			// Key format: _idx:cat:<category>:<record_key>
			parts := strings.Split(key, ":")
			if len(parts) >= 3 {
				cat := parts[2]
				if !HarvestedCategories[cat] {
					categories[cat]++
				}
				it.Next()
			} else {
				it.Next()
			}
		}
		return nil
	})
	return categories, err
}

// StandardsSymbolSummary represents a single symbol entry in the standards overview.
type StandardsSymbolSummary struct {
	Name       string `json:"name"`
	SymbolType string `json:"symbol_type"`
	Key        string `json:"key"`
}

// StandardsPackageOverview represents a package-level grouping of harvested symbols.
type StandardsPackageOverview struct {
	TotalSymbols  int                      `json:"total_symbols"`
	ByType        map[string]int           `json:"by_type"`
	Symbols       []StandardsSymbolSummary `json:"symbols"`
	HasPackageDoc bool                     `json:"has_package_doc"`
	Checksum      string                   `json:"checksum,omitempty,omitzero"`
}

// ListStandardsOverview returns a package-grouped overview of all harvested standards data.
func (s *MemoryStore) ListStandardsOverview(ctx context.Context, packageFilter string) (map[string]*StandardsPackageOverview, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	packages := make(map[string]*StandardsPackageOverview)

	err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		if err := s.scanHarvestedCodeIndex(ctx, txn, it, packageFilter, packages); err != nil {
			return err
		}
		s.scanPackageDocIndex(it, packageFilter, packages)
		s.scanSysDriftIndex(txn, it, packageFilter, packages)
		return nil
	})

	return packages, err
}

// scanHarvestedCodeIndex scans the _idx:cat:harvestedcode: prefix to build symbol summaries.
func (s *MemoryStore) scanHarvestedCodeIndex(ctx context.Context, txn *badger.Txn, it *badger.Iterator, packageFilter string, packages map[string]*StandardsPackageOverview) error {
	prefix := []byte("_idx:cat:harvestedcode:")
	for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := it.Item().Value(func(kVal []byte) error {
			recordKey := string(kVal)

			// Parse package and symbol name from key: pkg:<path>:<name>
			if !strings.HasPrefix(recordKey, "pkg:") {
				return nil
			}
			parts := strings.SplitN(recordKey[4:], ":", 2)
			if len(parts) < 2 {
				return nil
			}
			pkgPath := parts[0]
			symName := parts[1]

			// Apply package filter if set
			if packageFilter != "" && !strings.HasPrefix(pkgPath, packageFilter) {
				return nil
			}

			var symType string
			rec, getErr := loadRecordFromTxn(txn, kVal)
			if getErr == nil {
				for _, tag := range rec.Tags {
					if after, ok := strings.CutPrefix(tag, "type:"); ok {
						symType = after
						break
					}
				}
			}

			pkg := s.getOrCreatePackageOverview(packages, pkgPath)
			pkg.TotalSymbols++
			if symType != "" {
				pkg.ByType[symType]++
			}
			pkg.Symbols = append(pkg.Symbols, StandardsSymbolSummary{
				Name:       symName,
				SymbolType: symType,
				Key:        recordKey,
			})
			return nil
		}); err != nil {
			slog.Warn("Error reading standards index", "error", err)
		}
	}
	return nil
}

// scanPackageDocIndex scans the _idx:cat:packagedoc: prefix to flag packages with documentation.
func (s *MemoryStore) scanPackageDocIndex(it *badger.Iterator, packageFilter string, packages map[string]*StandardsPackageOverview) {
	pdPrefix := []byte("_idx:cat:packagedoc:")
	for it.Seek(pdPrefix); it.ValidForPrefix(pdPrefix); it.Next() {
		if err := it.Item().Value(func(kVal []byte) error {
			recordKey := string(kVal)
			if !strings.HasPrefix(recordKey, "pkg:") {
				return nil
			}
			parts := strings.SplitN(recordKey[4:], ":", 2)
			if len(parts) < 1 {
				return nil
			}
			pkgPath := parts[0]
			if packageFilter != "" && !strings.HasPrefix(pkgPath, packageFilter) {
				return nil
			}

			pkg := s.getOrCreatePackageOverview(packages, pkgPath)
			pkg.HasPackageDoc = true
			return nil
		}); err != nil {
			slog.Warn("Error reading PackageDoc index", "error", err)
		}
	}
}

// scanSysDriftIndex scans the _idx:cat:sysdrift: prefix to attach API checksums to packages.
func (s *MemoryStore) scanSysDriftIndex(txn *badger.Txn, it *badger.Iterator, packageFilter string, packages map[string]*StandardsPackageOverview) {
	driftPrefix := []byte("_idx:cat:sysdrift:")
	for it.Seek(driftPrefix); it.ValidForPrefix(driftPrefix); it.Next() {
		if err := it.Item().Value(func(kVal []byte) error {
			recordKey := string(kVal)
			// Key: pkg:<path>:CheckDrift
			if !strings.HasPrefix(recordKey, "pkg:") {
				return nil
			}
			trimmed := strings.TrimPrefix(recordKey, "pkg:")
			pkgPath := strings.TrimSuffix(trimmed, ":CheckDrift")

			if packageFilter != "" && !strings.HasPrefix(pkgPath, packageFilter) {
				return nil
			}

			pkg, ok := packages[pkgPath]
			if !ok {
				return nil
			}

			rec, getErr := loadRecordFromTxn(txn, kVal)
			if getErr == nil {
				pkg.Checksum = rec.Content
			}
			return nil
		}); err != nil {
			slog.Warn("Error reading SysDrift index", "error", err)
		}
	}
}

// getOrCreatePackageOverview returns an existing overview or creates a new one.
func (s *MemoryStore) getOrCreatePackageOverview(packages map[string]*StandardsPackageOverview, pkgPath string) *StandardsPackageOverview {
	pkg, ok := packages[pkgPath]
	if !ok {
		pkg = &StandardsPackageOverview{
			ByType:  make(map[string]int),
			Symbols: []StandardsSymbolSummary{},
		}
		packages[pkgPath] = pkg
	}
	return pkg
}

func recordMatchesDomainSearch(q SearchDomainQuery, rec *Record) bool {
	if rec.Category == catSysDrift {
		return false
	}
	if q.SymbolType != "" && !slices.Contains(rec.Tags, "type:"+q.SymbolType) {
		return false
	}
	if q.Interface != "" && !slices.Contains(rec.Tags, "implements:"+q.Interface) {
		return false
	}
	if q.Receiver != "" && !slices.Contains(rec.Tags, "receiver:"+q.Receiver) {
		return false
	}
	if q.Domain != "" && !slices.Contains(rec.Tags, "domain:"+q.Domain) {
		return false
	}
	if len(q.Tags) > 0 {
		if q.TagMatchMode == fieldAny {
			match := false
			for _, reqTag := range q.Tags {
				if slices.Contains(rec.Tags, reqTag) {
					match = true
					break
				}
			}
			if !match {
				return false
			}
		} else {
			for _, reqTag := range q.Tags {
				if !slices.Contains(rec.Tags, reqTag) {
					return false
				}
			}
		}
	}
	return true
}

func buildSearchDomainRequiredTags(q SearchDomainQuery) []string {
	var requiredTags []string
	if q.SymbolType != "" {
		requiredTags = append(requiredTags, "type:"+q.SymbolType)
	}
	if q.Interface != "" {
		requiredTags = append(requiredTags, "implements:"+q.Interface)
	}
	if q.Receiver != "" {
		requiredTags = append(requiredTags, "receiver:"+q.Receiver)
	}
	if q.Domain != "" {
		requiredTags = append(requiredTags, "domain:"+q.Domain)
	}
	if q.PackageFilter != "" {
		requiredTags = append(requiredTags, "package:"+q.PackageFilter)
	}
	requiredTags = append(requiredTags, "domain:"+q.TargetDomain)
	if len(q.Tags) > 0 && q.TagMatchMode != fieldAny {
		requiredTags = append(requiredTags, q.Tags...)
	}
	return requiredTags
}

func bleveHitMatchesDomainSearch(q SearchDomainQuery, hitID string, rec *Record) bool {
	if q.PackageFilter != "" && !strings.HasPrefix(hitID, "pkg:"+q.PackageFilter) {
		return false
	}
	if q.KeyPrefix != "" && !strings.HasPrefix(hitID, q.KeyPrefix) {
		return false
	}
	if q.KeySuffix != "" && !strings.HasSuffix(hitID, q.KeySuffix) {
		return false
	}
	if len(q.Tags) > 0 && q.TagMatchMode == fieldAny {
		for _, tag := range q.Tags {
			if slices.Contains(rec.Tags, tag) {
				return true
			}
		}
		return false
	}
	return true
}

// SearchDomainQuery defines parameters for domain-scoped search.
type SearchDomainQuery struct {
	TargetDomain  string
	Query         string
	PackageFilter string
	SymbolType    string
	Interface     string
	Receiver      string
	Domain        string
	Limit         int
	KeyPrefix     string
	KeySuffix     string
	Tags          []string
	TagMatchMode  string
}

// SearchStandards performs a standards-scoped search with multi-dimensional tag filtering.
// Natively leverages the Bleve inverted index if a text query is present.
func (s *MemoryStore) SearchStandards(ctx context.Context, q SearchDomainQuery) (iter.Seq[*SearchResult], error) {
	q.TargetDomain = DomainStandards
	return s.SearchDomain(ctx, q)
}

// ListDomainOverview returns a package-grouped overview of harvested data for a specific domain.
// This is the domain-parameterized equivalent of ListStandardsOverview.
//
//nolint:gocognit // ListDomainOverview coordinates multi-phase Badger/index maintenance.
func (s *MemoryStore) ListDomainOverview(ctx context.Context, targetDomain string, packageFilter string) (map[string]*StandardsPackageOverview, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	packages := make(map[string]*StandardsPackageOverview)

	err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		prefix := []byte("_idx:domain:" + targetDomain + ":")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			if err := it.Item().Value(func(kVal []byte) error {
				recordKey := string(kVal)

				// Parse package and symbol name from key: pkg:<path>:<name>
				if !strings.HasPrefix(recordKey, "pkg:") {
					return nil
				}

				rec, getErr := loadRecordFromTxn(txn, kVal)
				if getErr == nil && rec.Domain == targetDomain {
					parts := strings.SplitN(recordKey[4:], ":", 2)
					if len(parts) >= 2 {
						pkgPath := parts[0]
						symName := parts[1]

						if packageFilter == "" || strings.HasPrefix(pkgPath, packageFilter) {
							switch rec.Category {
							case catHarvestedCode:
								var symType string
								for _, tag := range rec.Tags {
									if after, ok := strings.CutPrefix(tag, "type:"); ok {
										symType = after
										break
									}
								}
								pkg := s.getOrCreatePackageOverview(packages, pkgPath)
								pkg.TotalSymbols++
								if symType != "" {
									pkg.ByType[symType]++
								}
								pkg.Symbols = append(pkg.Symbols, StandardsSymbolSummary{
									Name:       symName,
									SymbolType: symType,
									Key:        recordKey,
								})
							case "PackageDoc":
								pkg := s.getOrCreatePackageOverview(packages, pkgPath)
								pkg.HasPackageDoc = true
							case catSysDrift:
								if pkg, ok := packages[pkgPath]; ok {
									pkg.Checksum = rec.Content
								}
							}
						}
					}
				}
				return nil
			}); err != nil {
				slog.Warn("Error reading domain index", "domain", targetDomain, "error", err)
			}
		}
		return nil
	})

	return packages, err
}

// SearchDomain performs a domain-scoped search with multi-dimensional tag filtering.
// Natively leverages the Bleve inverted index if a text query is present.
//
//nolint:gocognit // SearchDomain coordinates multi-phase Badger/index maintenance.
func (s *MemoryStore) SearchDomain(ctx context.Context, q SearchDomainQuery) (iter.Seq[*SearchResult], error) {
	s.mu.RLock()
	searchEngine := s.search
	s.mu.RUnlock()

	// 1. Bleve Routing (Fast Path)
	if q.Query != "" && searchEngine != nil {
		requiredTags := buildSearchDomainRequiredTags(q)
		hits, err := searchEngine.SearchScoped(ctx, q.Query, nil, requiredTags, q.Limit)
		if err == nil {
			return func(yield func(*SearchResult) bool) {
				count := 0
				if viewErr := s.db.View(func(txn *badger.Txn) error {
					for _, h := range hits {
						rec, gErr := s.Get(ctx, h.ID)
						if gErr == nil && bleveHitMatchesDomainSearch(q, h.ID, rec) {
							count++
							s.cacheHits.Add(1)
							if !yield(&SearchResult{Key: h.ID, Record: rec, Score: int(h.Score * 100), Snippets: h.Snippets}) {
								break
							}
							if q.Limit > 0 && count >= q.Limit {
								break
							}
						}
					}
					return nil
				}); viewErr != nil {
					slog.Warn("Domain search bleve hydration view failed", "domain", q.TargetDomain, "error", viewErr)
				}
				if count == 0 {
					s.cacheMisses.Add(1)
				}
			}, nil
		}
		slog.Warn("Bleve domain search failed, falling back to badger linear scan", "domain", q.TargetDomain, "error", err)
	}

	// 2. Badger Linear Scan (Fallback Path)
	s.mu.RLock()
	defer s.mu.RUnlock()

	return func(yield func(*SearchResult) bool) {
		var candidates []*SearchResult

		if viewErr := s.db.View(func(txn *badger.Txn) error {
			it := txn.NewIterator(badger.DefaultIteratorOptions)
			defer it.Close()

			prefix := []byte("_idx:domain:" + q.TargetDomain + ":")
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}

				if err := it.Item().Value(func(kVal []byte) error {
					recordKey := string(kVal)

					// Apply Key guards proactively before fetching payload
					if q.KeyPrefix != "" && !strings.HasPrefix(recordKey, q.KeyPrefix) {
						return nil
					}
					if q.KeySuffix != "" && !strings.HasSuffix(recordKey, q.KeySuffix) {
						return nil
					}

					// Package filter on key prefix proactively
					if q.PackageFilter != "" {
						if !strings.HasPrefix(recordKey, "pkg:"+q.PackageFilter) {
							return nil
						}
					}

					rec, getErr := loadRecordFromTxn(txn, kVal)
					if getErr == nil && recordMatchesDomainSearch(q, rec) {
						candidates = append(candidates, &SearchResult{Key: recordKey, Record: rec})
					}
					return nil
				}); err != nil {
					slog.Warn("Error during domain search", "domain", q.TargetDomain, "error", err)
				}
			}
			return nil
		}); viewErr != nil {
			slog.Warn("Domain search fallback view failed", "domain", q.TargetDomain, "error", viewErr)
		}

		// Apply text query ranking if specified
		var final []*SearchResult
		if q.Query == "" {
			final = candidates
		} else {
			final = s.rankCandidates(ctx, q.Query, candidates)
		}

		if q.Limit > 0 && len(final) > q.Limit {
			final = final[:q.Limit]
		}

		if len(final) > 0 {
			s.cacheHits.Add(uint64(len(final)))
			for _, f := range final {
				if !yield(f) {
					break
				}
			}
		} else {
			s.cacheMisses.Add(1)
		}
	}, nil
}

func computeJaccard(a, b string) float64 {
	clean := func(s string) string {
		return strings.Map(func(r rune) rune {
			if strings.ContainsRune("!.,:;?()[]{}", r) {
				return -1
			}
			return r
		}, strings.ToLower(s))
	}

	setA := make(map[string]struct{})
	for w := range strings.FieldsSeq(clean(a)) {
		if len(w) > 2 {
			setA[w] = struct{}{}
		}
	}
	setB := make(map[string]struct{})
	for w := range strings.FieldsSeq(clean(b)) {
		if len(w) > 2 {
			setB[w] = struct{}{}
		}
	}
	if len(setA) == 0 || len(setB) == 0 {
		return 0
	}
	inter := 0
	for w := range setA {
		if _, ok := setB[w]; ok {
			inter++
		}
	}
	union := len(setA) + len(setB) - inter
	return float64(inter) / float64(union)
}

func closeExportFile(f *os.File, stage string) {
	if err := f.Close(); err != nil {
		slog.Warn("failed to close export file", "stage", stage, "error", err)
	}
}

// ExportJSONL iterates through the badger DB and streams each record as a JSON line
// to the target file. It enforces os.O_EXCL to prevent overwriting existing files.
func (s *MemoryStore) ExportJSONL(ctx context.Context, safePath string, filterCategory string, filterTags []string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	safePath = filepath.Clean(safePath)
	// 1. Open destination file with strict O_EXCL to prevent overwrite vulnerabilities
	f, err := os.OpenFile(safePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // safePath is cleaned and validated by caller-side sandboxing
	if err != nil {
		return 0, fmt.Errorf("failed to open export target (O_EXCL constraint): %w", err)
	}

	// Buffer writes to reduce per-record syscalls (~64x reduction for large exports).
	bw := bufio.NewWriterSize(f, 64*1024)
	count := 0

	err = s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 100
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			k := string(item.Key())

			// Skip internal index keys
			if strings.HasPrefix(k, "_idx:") {
				continue
			}

			var rec *Record
			err := item.Value(func(v []byte) error {
				var mErr error
				rec, mErr = migrateRecord(v)
				return mErr
			})
			if err != nil {
				slog.Warn("Failed to unmarshal record during export", "key", k, "error", err)
				continue
			}

			if !matchesExportFilters(rec, filterCategory, filterTags) {
				continue
			}

			if err := writeJSONLRecord(bw, k, rec); err != nil {
				return err
			}
			count++
		}
		return nil
	})

	if err != nil {
		closeExportFile(f, "iteration-failed")
		return count, fmt.Errorf("export iteration failed: %w", err)
	}

	if err := bw.Flush(); err != nil {
		closeExportFile(f, "flush-failed")
		return count, fmt.Errorf("failed to flush export buffer: %w", err)
	}
	if err := f.Sync(); err != nil {
		closeExportFile(f, "sync-failed")
		return count, fmt.Errorf("failed to sync export file to disk: %w", err)
	}
	if err := f.Close(); err != nil {
		return count, fmt.Errorf("failed to close export file: %w", err)
	}

	return count, nil
}

// matchesExportFilters checks if a record passes the category and tag filters.
func matchesExportFilters(rec *Record, filterCategory string, filterTags []string) bool {
	if filterCategory != "" && rec.Category != filterCategory {
		return false
	}
	if len(filterTags) > 0 {
		for _, reqTag := range filterTags {
			found := slices.Contains(rec.Tags, reqTag)
			if !found {
				return false
			}
		}
	}
	return true
}

// writeJSONLRecord marshals a record with its key and writes it as a JSON line.
func writeJSONLRecord(w io.Writer, key string, rec *Record) error {
	exportObj := struct {
		Key string `json:"key"`
		Record
	}{
		Key:    key,
		Record: *rec,
	}

	b, err := json.Marshal(exportObj)
	if err != nil {
		slog.Warn("Failed to marshal record for export", "key", key, "error", err)
		return nil // skip corrupt record, don't abort export
	}

	if _, writeErr := w.Write(append(b, '\n')); writeErr != nil {
		return fmt.Errorf("failed to write jsonl stream: %w", writeErr)
	}
	return nil
}

// ImportJSONL reads a JSONL file from disk and imports it into the Badger DB,
// buffering 100 entries at a time to remain memory-flat and taking advantage of atomic batch writes.
func (s *MemoryStore) ImportJSONL(ctx context.Context, safePath string, mergeStrategy string) (int, []BatchError, error) {
	// 1. Open the file
	f, err := os.Open(safePath) //nolint:gosec // safePath is validated by caller-side sandboxing
	if err != nil {
		return 0, nil, fmt.Errorf("failed to open import target: %w", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			slog.Warn("failed to close import file", "error", closeErr)
		}
	}()

	var totalStored int
	var allErrors []BatchError

	buffer := make([]BatchEntry, 0, 100)
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var raw struct {
			Key        string    `json:"key"`
			Title      string    `json:"title,omitempty,omitzero"`
			SymbolName string    `json:"symbolname,omitempty,omitzero"`
			Content    string    `json:"content"`
			Value      string    `json:"value"`
			Category   string    `json:"category"`
			Domain     string    `json:"domain"`
			SessionID  string    `json:"session_id,omitempty,omitzero"`
			SourcePath string    `json:"source_path,omitempty,omitzero"`
			SourceHash string    `json:"source_hash,omitempty,omitzero"`
			Tags       []string  `json:"tags"`
			CreatedAt  time.Time `json:"created_at"`
			UpdatedAt  time.Time `json:"updated_at"`
		}

		if err := json.Unmarshal(line, &raw); err != nil {
			slog.Warn("JSONL unmarshal failed during import", "lineNum", lineNum, "error", err)
			allErrors = append(allErrors, BatchError{
				Key:   "line-" + fmt.Sprint(lineNum),
				Error: err.Error(),
			})
			continue
		}

		content := raw.Content
		if content == "" {
			content = raw.Value
		}

		entry := BatchEntry{
			Key:        raw.Key,
			Title:      raw.Title,
			SymbolName: raw.SymbolName,
			Value:      content,
			Category:   raw.Category,
			Domain:     raw.Domain,
			SessionID:  raw.SessionID,
			SourcePath: raw.SourcePath,
			SourceHash: raw.SourceHash,
			Tags:       raw.Tags,
			CreatedAt:  raw.CreatedAt,
			UpdatedAt:  raw.UpdatedAt,
		}

		buffer = append(buffer, entry)

		if len(buffer) >= 100 {
			stored, errs, bErr := s.SaveBatch(ctx, buffer)
			if bErr != nil {
				return totalStored, allErrors, fmt.Errorf("batch insert failed at line %d: %w", lineNum, bErr)
			}
			totalStored += stored
			allErrors = append(allErrors, errs...)

			// Reset buffer efficiently
			buffer = buffer[:0]
		}
	}

	if err := scanner.Err(); err != nil {
		allErrors = append(allErrors, BatchError{Key: "scanner-err", Error: err.Error()})
		slog.Error("JSONL scanner encountered an error before EOF", "error", err)
	}

	// Flush remaining buffer
	if len(buffer) > 0 {
		stored, errs, bErr := s.SaveBatch(ctx, buffer)
		if bErr != nil {
			return totalStored, allErrors, fmt.Errorf("final batch insert failed: %w", bErr)
		}
		totalStored += stored
		allErrors = append(allErrors, errs...)
	}

	return totalStored, allErrors, nil
}

// DeleteStandards removes standards by category or specific package path prefix.
//
//nolint:gocognit // DeleteStandards coordinates multi-phase Badger/index maintenance.
func (s *MemoryStore) DeleteStandards(ctx context.Context, category, pkg string) (int, error) {
	// Allow empty category and pkg to denote a global domain sweep
	if category != "" && !HarvestedCategories[category] {
		return 0, fmt.Errorf("category %q is not a valid standards category", category)
	}

	// Global Domain Sweep Delegation - move before lock to avoid deadlock
	if category == "" && pkg == "" {
		return s.DeleteDomain(ctx, DomainStandards)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var keysToDelete []string
	var deletedCount int

	// Define a helper to safely parse the Record to delete indices
	getRecordForDeletion := func(txn *badger.Txn, key string) (*Record, error) {
		item, err := txn.Get([]byte(key))
		if err != nil {
			return nil, err
		}
		var rec *Record
		if vErr := item.Value(func(v []byte) error {
			if r, mErr := migrateRecord(v); mErr == nil {
				rec = r
			}
			return nil
		}); vErr != nil {
			slog.Warn("Failed to read record value during standards deletion", "key", key, "error", vErr)
		}
		return rec, nil
	}

	// First pass: collect matching domains logic
	if category != "" {
		if err := s.db.View(func(txn *badger.Txn) error {
			it := txn.NewIterator(badger.DefaultIteratorOptions)
			defer it.Close()
			prefix := fmt.Appendf(nil, "_idx:cat:%s:", strings.ToLower(category))
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				if vErr := it.Item().Value(func(kVal []byte) error {
					key := string(kVal)
					if pkg != "" && !strings.HasPrefix(key, "pkg:"+pkg+":") {
						return nil
					}
					keysToDelete = append(keysToDelete, key)
					return nil
				}); vErr != nil {
					slog.Warn("Failed to read index value during standards delete", "error", vErr)
				}
			}
			return nil
		}); err != nil {
			slog.Warn("Failed to scan category index during standards delete", "category", category, "error", err)
		}
	} else if pkg != "" {
		if err := s.db.View(func(txn *badger.Txn) error {
			it := txn.NewIterator(badger.DefaultIteratorOptions)
			defer it.Close()
			prefix := []byte("pkg:" + pkg + ":")
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				key := string(it.Item().Key())
				if rec, err := getRecordForDeletion(txn, key); err == nil && rec != nil {
					if HarvestedCategories[rec.Category] {
						keysToDelete = append(keysToDelete, key)
					}
				}
			}
			return nil
		}); err != nil {
			slog.Warn("Failed to scan package prefix during standards delete", "pkg", pkg, "error", err)
		}
	}

	// Delete in chunks to avoid ErrTxnTooBig
	batchSize := 500
	for i := 0; i < len(keysToDelete); i += batchSize {
		end := min(i+batchSize, len(keysToDelete))
		chunk := keysToDelete[i:end]

		err := s.UpdateWithRetry(func(txn *badger.Txn) error {
			for _, key := range chunk {
				if err := s.deleteNoLockTxn(txn, key); err == nil {
					deletedCount++
				} else if !errors.Is(err, badger.ErrKeyNotFound) {
					return err
				}
			}
			return nil
		})
		if err != nil {
			slog.Error("Failed to delete a batch of standards", "error", err)
		}
	}

	// Purge from Bleve search index
	if s.search != nil && len(keysToDelete) > 0 {
		for start := 0; start < len(keysToDelete); start += s.maxBatchSize {
			end := min(start+s.maxBatchSize, len(keysToDelete))
			chunk := keysToDelete[start:end]
			if dErr := s.search.DeleteBatch(chunk); dErr != nil {
				slog.Warn("Failed to purge batch from search index", "error", dErr)
			}
		}
	}

	slog.Info("Deleted standards", "category", category, "package", pkg, "count", deletedCount)
	return deletedCount, nil
}

// DeleteProjects removes projects by category or specific package path prefix.
//
//nolint:gocognit // DeleteProjects coordinates multi-phase Badger/index maintenance.
func (s *MemoryStore) DeleteProjects(ctx context.Context, category, pkg string) (int, error) {
	// Allow empty category and pkg to denote a global domain sweep
	if category != "" && !HarvestedCategories[category] {
		return 0, fmt.Errorf("category %q is not a valid projects category", category)
	}

	// Global Domain Sweep Delegation - move before lock to avoid deadlock
	if category == "" && pkg == "" {
		return s.DeleteDomain(ctx, DomainProjects)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var keysToDelete []string
	var deletedCount int

	getRecordForDeletion := func(txn *badger.Txn, key string) (*Record, error) {
		item, err := txn.Get([]byte(key))
		if err != nil {
			return nil, err
		}
		var rec *Record
		if vErr := item.Value(func(v []byte) error {
			if r, mErr := migrateRecord(v); mErr == nil {
				rec = r
			}
			return nil
		}); vErr != nil {
			slog.Warn("Failed to read record value during projects deletion", "key", key, "error", vErr)
		}
		return rec, nil
	}

	if category != "" {
		if err := s.db.View(func(txn *badger.Txn) error {
			it := txn.NewIterator(badger.DefaultIteratorOptions)
			defer it.Close()
			prefix := fmt.Appendf(nil, "_idx:cat:%s:", strings.ToLower(category))
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				if vErr := it.Item().Value(func(kVal []byte) error {
					key := string(kVal)
					if pkg != "" && !strings.HasPrefix(key, "pkg:"+pkg+":") {
						return nil
					}
					if rec, err := getRecordForDeletion(txn, key); err == nil && rec != nil {
						if rec.Domain == DomainProjects {
							keysToDelete = append(keysToDelete, key)
						}
					}
					return nil
				}); vErr != nil {
					slog.Warn("Failed to read index value during projects delete", "error", vErr)
				}
			}
			return nil
		}); err != nil {
			slog.Warn("Failed to scan category index during projects delete", "category", category, "error", err)
		}
	} else if pkg != "" {
		if err := s.db.View(func(txn *badger.Txn) error {
			it := txn.NewIterator(badger.DefaultIteratorOptions)
			defer it.Close()
			prefix := []byte("pkg:" + pkg + ":")
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				key := string(it.Item().Key())
				if rec, err := getRecordForDeletion(txn, key); err == nil && rec != nil {
					if rec.Domain == DomainProjects {
						keysToDelete = append(keysToDelete, key)
					}
				}
			}
			return nil
		}); err != nil {
			slog.Warn("Failed to scan package prefix during projects delete", "pkg", pkg, "error", err)
		}
	}

	batchSize := 500
	for i := 0; i < len(keysToDelete); i += batchSize {
		end := min(i+batchSize, len(keysToDelete))
		chunk := keysToDelete[i:end]

		err := s.UpdateWithRetry(func(txn *badger.Txn) error {
			for _, key := range chunk {
				if err := s.deleteNoLockTxn(txn, key); err == nil {
					deletedCount++
				} else if !errors.Is(err, badger.ErrKeyNotFound) {
					return err
				}
			}
			return nil
		})
		if err != nil {
			slog.Error("Failed to delete a batch of projects", "error", err)
		}
	}

	if s.search != nil && len(keysToDelete) > 0 {
		for start := 0; start < len(keysToDelete); start += s.maxBatchSize {
			end := min(start+s.maxBatchSize, len(keysToDelete))
			chunk := keysToDelete[start:end]
			if dErr := s.search.DeleteBatch(chunk); dErr != nil {
				slog.Warn("Failed to purge batch from search index", "error", dErr)
			}
		}
	}

	slog.Info("Deleted projects", "category", category, "package", pkg, "count", deletedCount)
	return deletedCount, nil
}

// PurgeDomain completely deletes all records associated with a specific domain.
func (s *MemoryStore) PurgeDomain(ctx context.Context, targetDomain string) (int, error) {
	if targetDomain == "" {
		return 0, fmt.Errorf("targetDomain is missing")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var keysToDelete []string
	var deletedCount int

	if err := s.db.View(func(txn *badger.Txn) error {
		// Fix #7: Use domain index prefix scan instead of full table scan.
		prefix := []byte("_idx:domain:" + targetDomain + ":")
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			idxKey := string(it.Item().Key())
			actualKey := strings.TrimPrefix(idxKey, string(prefix))
			if actualKey != "" {
				keysToDelete = append(keysToDelete, actualKey)
			}
		}
		return nil
	}); err != nil {
		return 0, fmt.Errorf("purge scan failed: %w", err)
	}

	batchSize := 500
	for i := 0; i < len(keysToDelete); i += batchSize {
		end := min(i+batchSize, len(keysToDelete))
		chunk := keysToDelete[i:end]

		err := s.UpdateWithRetry(func(txn *badger.Txn) error {
			for _, key := range chunk {
				if err := s.deleteNoLockTxn(txn, key); err == nil {
					deletedCount++
				} else if !errors.Is(err, badger.ErrKeyNotFound) {
					return err
				}
			}
			return nil
		})
		if err != nil {
			slog.Error("Failed to purge a batch of records", "domain", targetDomain, "error", err)
		}
	}

	if s.search != nil && len(keysToDelete) > 0 {
		for start := 0; start < len(keysToDelete); start += s.maxBatchSize {
			end := min(start+s.maxBatchSize, len(keysToDelete))
			if dErr := s.search.DeleteBatch(keysToDelete[start:end]); dErr != nil {
				slog.Warn("Bleve batch delete failed during domain purge", "error", dErr)
			}
		}
	}

	slog.Info("Domain completely purged", "domain", targetDomain, "count", deletedCount)
	return deletedCount, nil
}

// PruneDomain deletes records from a domain (or all domains if empty) whose UpdatedAt exceeds daysOld.
//
//nolint:gocognit // PruneDomain coordinates multi-phase Badger/index maintenance.
func (s *MemoryStore) PruneDomain(ctx context.Context, targetDomain string, daysOld int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var keysToDelete []string
	var deletedCount int
	cutoffTime := time.Now().Add(-time.Duration(daysOld) * 24 * time.Hour)

	getRecordForDeletion := func(txn *badger.Txn, key string) (*Record, error) {
		item, err := txn.Get([]byte(key))
		if err != nil {
			return nil, err
		}
		var rec *Record
		if vErr := item.Value(func(v []byte) error {
			if r, mErr := migrateRecord(v); mErr == nil {
				rec = r
			}
			return nil
		}); vErr != nil {
			slog.Warn("Failed to read record during prune scan", "error", vErr)
		}
		return rec, nil
	}

	if err := s.db.View(func(txn *badger.Txn) error {
		// Fix #8: Use domain index prefix scan when targetDomain is specified.
		// When targetDomain is empty (global prune), fall back to full scan.
		if targetDomain != "" {
			prefix := []byte("_idx:domain:" + targetDomain + ":")
			opts := badger.DefaultIteratorOptions
			opts.PrefetchValues = false
			it := txn.NewIterator(opts)
			defer it.Close()
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				idxKey := string(it.Item().Key())
				actualKey := strings.TrimPrefix(idxKey, string(prefix))
				if actualKey == "" {
					continue
				}
				// Still need to read the record for time-based filtering
				if rec, err := getRecordForDeletion(txn, actualKey); err == nil && rec != nil {
					if rec.UpdatedAt.Before(cutoffTime) {
						keysToDelete = append(keysToDelete, actualKey)
					}
				}
			}
		} else {
			// Global prune: full scan is correct since all domains are eligible
			it := txn.NewIterator(badger.DefaultIteratorOptions)
			defer it.Close()
			for it.Rewind(); it.Valid(); it.Next() {
				key := string(it.Item().Key())
				if strings.HasPrefix(key, "_idx:") {
					continue
				}
				if rec, err := getRecordForDeletion(txn, key); err == nil && rec != nil {
					if rec.UpdatedAt.Before(cutoffTime) {
						keysToDelete = append(keysToDelete, key)
					}
				}
			}
		}
		return nil
	}); err != nil {
		return 0, fmt.Errorf("prune scan failed: %w", err)
	}

	batchSize := 500
	for i := 0; i < len(keysToDelete); i += batchSize {
		end := min(i+batchSize, len(keysToDelete))
		chunk := keysToDelete[i:end]

		err := s.UpdateWithRetry(func(txn *badger.Txn) error {
			for _, key := range chunk {
				if err := s.deleteNoLockTxn(txn, key); err == nil {
					deletedCount++
				} else if !errors.Is(err, badger.ErrKeyNotFound) {
					return err
				}
			}
			return nil
		})
		if err != nil {
			slog.Error("Failed to prune a batch of records", "domain", targetDomain, "error", err)
		}
	}

	if s.search != nil && len(keysToDelete) > 0 {
		for start := 0; start < len(keysToDelete); start += s.maxBatchSize {
			end := min(start+s.maxBatchSize, len(keysToDelete))
			if dErr := s.search.DeleteBatch(keysToDelete[start:end]); dErr != nil {
				slog.Warn("Bleve batch delete failed during domain prune", "error", dErr)
			}
		}
	}

	slog.Info("Namespace pruned successfully", "domain", targetDomain, "daysOlderThan", daysOld, "count", deletedCount)
	return deletedCount, nil
}

// AttributeQuery defines parameters for programmatic domain retrieval.
type AttributeQuery struct {
	Tags         []string
	TagMatchMode string // "all" or fieldAny
	SessionID    string
	SymbolName   string
	SourcePath   string
	Category     string
}

// GetByAttributes scans a domain for records matching specific attribute filters.
//
//nolint:gocognit // GetByAttributes coordinates multi-phase Badger/index maintenance.
func (s *MemoryStore) GetByAttributes(ctx context.Context, domain string, query *AttributeQuery) (map[string]*Record, error) {
	if domain == "" {
		return nil, fmt.Errorf("domain is required for attribute scanning")
	}

	results := make(map[string]*Record)
	err := s.db.View(func(txn *badger.Txn) error {
		prefixStr := fmt.Sprintf("_idx:domain:%s:", domain)
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte(prefixStr)
		opts.PrefetchValues = true

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			idxItem := it.Item()
			actualKey, err := idxItem.ValueCopy(nil)
			if err != nil {
				continue
			}

			// Fetch the actual record
			recItem, err := txn.Get(actualKey)
			if err != nil {
				continue
			}

			var rec *Record
			err = recItem.Value(func(v []byte) error {
				r, mErr := migrateRecord(v)
				if mErr != nil {
					return mErr
				}
				rec = r
				return nil
			})
			if err != nil || rec == nil {
				continue
			}

			// Apply Filters
			if query.SessionID != "" && rec.SessionID != query.SessionID {
				continue
			}
			if query.SymbolName != "" && rec.SymbolName != query.SymbolName {
				continue
			}
			if query.SourcePath != "" && rec.SourcePath != query.SourcePath {
				continue
			}
			if query.Category != "" && rec.Category != query.Category {
				continue
			}

			if len(query.Tags) > 0 {
				// Combine outer tags with potential inner JSON tags
				allTags := append([]string{}, rec.Tags...)
				var innerData struct {
					Tags []string `json:"tags"`
				}
				if err := json.Unmarshal([]byte(rec.Content), &innerData); err == nil && len(innerData.Tags) > 0 {
					allTags = append(allTags, innerData.Tags...)
				}

				matched := 0
				for _, qtag := range query.Tags {
					if slices.Contains(allTags, qtag) {
						matched++
					}
				}
				if query.TagMatchMode == fieldAny {
					if matched == 0 {
						continue
					}
				} else { // default to "all"
					if matched < len(query.Tags) {
						continue
					}
				}
			}

			results[string(actualKey)] = rec
		}
		return nil
	})

	return results, err
}

// VacuumProjects scans the projects domain for structural integrity.
// Currently it checks for corrupted entries and prepares for future project-level schema rules.
func (s *MemoryStore) VacuumProjects(ctx context.Context, reportOnly bool) (*VacuumReport, error) {
	report := &VacuumReport{
		Namespace:  DomainProjects,
		ReportOnly: reportOnly,
	}

	if err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		prefix := []byte("_idx:domain:projects:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			if err := it.Item().Value(func(kVal []byte) error {
				rec, getErr := loadRecordFromTxn(txn, kVal)
				if getErr == nil && rec.Domain == DomainProjects {
					report.TotalScanned++
				}
				return nil
			}); err != nil {
				slog.Warn("Error scanning projects during vacuum", "error", err)
			}
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("projects vacuum scan failed: %w", err)
	}

	return report, nil
}
