// Package telemetry provides functionality for the telemetry subsystem.
package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"

	"github.com/maccavelli/mcp-server-recall/internal/config"
	"github.com/maccavelli/mcp-server-recall/internal/memory"
)

// StorageStats reports BadgerDB on-disk footprint, split by LSM tree and value log.
type StorageStats struct {
	LsmKB  float64 `json:"lsm_kb"`
	VlogKB float64 `json:"vlog_kb"`
}

// BleveStats reports full-text index size, document count, and drift from the datastore.
type BleveStats struct {
	Documents uint64 `json:"documents"`
	Drift     uint64 `json:"drift"`
	IndexSize int64  `json:"index_size"`
}

// AnalyticsStats reports cache effectiveness, RPC latency, and per-primitive call counts.
type AnalyticsStats struct {
	CacheHits       uint64                          `json:"cache_hits"`
	CacheMisses     uint64                          `json:"cache_misses"`
	DBHits          uint64                          `json:"db_hits"`
	DBMisses        uint64                          `json:"db_misses"`
	AvgRPCLatencyMs int64                           `json:"avg_rpc_latency_ms"`
	RPCPayloadBytes uint64                          `json:"rpc_payload_bytes"`
	Primitives      map[string]memory.PrimitiveStat `json:"primitives"`
}

// WriteOpsStats counts record writes by outcome: newly created, updated, or merged.
type WriteOpsStats struct {
	Created uint64 `json:"created"`
	Updated uint64 `json:"updated"`
	Merged  uint64 `json:"merged"`
}

// BatchHealthStats reports throughput and error totals for batched writes.
type BatchHealthStats struct {
	EntriesProcessed uint64 `json:"entries_processed"`
	Errors           uint64 `json:"errors"`
}

// CategoryEntry is a single category and its record count in the distribution report.
type CategoryEntry struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
}

// MemoryGCStats reports retention sweeps, pruned nodes, and record counts by age horizon.
type MemoryGCStats struct {
	Sweeps      uint64 `json:"sweeps"`
	PrunedNodes uint64 `json:"pruned_nodes"`
	Horizon24h  int    `json:"horizon_24h"`
	Horizon7d   int    `json:"horizon_7d"`
	Horizon30d  int    `json:"horizon_30d"`
}

// NetworkStats reports transport state: stdio attachment, HTTP port, and connected clients.
type NetworkStats struct {
	StdioConnected bool              `json:"stdio_connected"`
	HTTPPort       int               `json:"http_port"`
	HTTPClients    map[string]string `json:"http_clients"`
	TotalClients   int               `json:"total_clients"`
}

// SecurityStats counts namespace boundary violations rejected by the store.
type SecurityStats struct {
	BoundaryViolations uint64 `json:"boundary_violations"`
}

// ConfigStats reports the effective runtime configuration surfaced to the dashboard.
type ConfigStats struct {
	DBPath        string `json:"db_path"`
	Version       string `json:"version"`
	LogLevel      string `json:"log_level"`
	EnvGomemlimit string `json:"env_gomemlimit"`
}

// RuntimeStats reports Go runtime health: allocation, goroutines, uptime, GC and CPU.
type RuntimeStats struct {
	MemoryMB   uint64  `json:"memory_mb"`
	Goroutines int     `json:"goroutines"`
	UptimeSec  int64   `json:"uptime_sec"`
	NumGC      uint32  `json:"num_gc"`
	CPUUsage   float64 `json:"cpu_usage"`
}

// Snapshot is the complete telemetry document serialised to the ring buffer and
// read by the dashboard.
type Snapshot struct {
	Storage     StorageStats       `json:"storage"`
	Bleve       BleveStats         `json:"bleve"`
	Taxonomy    map[string]int     `json:"taxonomy"`
	Analytics   AnalyticsStats     `json:"analytics"`
	WriteOps    WriteOpsStats      `json:"write_ops"`
	BatchHealth BatchHealthStats   `json:"batch_health"`
	Categories  []CategoryEntry    `json:"categories"`
	MemoryGC    MemoryGCStats      `json:"memory_gc"`
	Network     NetworkStats       `json:"network"`
	Security    SecurityStats      `json:"security"`
	Config      ConfigStats        `json:"config"`
	Runtime     RuntimeStats       `json:"runtime"`
	TopQueries  []memory.QueryStat `json:"top_queries"`
}

var (
	ringMu sync.Mutex

	// StartTime is the process start instant, used to derive reported uptime.
	StartTime = time.Now()

	// Category distribution cadence gate (protected by ringMu).
	categoryTick   int
	lastCategories []CategoryEntry
)

// StartTelemetryLoop launches the periodic telemetry snapshot writer.
func StartTelemetryLoop(ctx context.Context, cfg *config.Config, store *memory.MemoryStore, logStream func() string, topQueries func(int) []memory.QueryStat, networkInfo func() NetworkStats) {
	ticker := time.NewTicker(10 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				WriteSnapshot(cfg, store, logStream, topQueries, networkInfo)
			}
		}
	}()
}

func dirSize(path string) int64 {
	var size int64
	if walkErr := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			size += info.Size()
		}
		return err
	}); walkErr != nil {
		return size
	}
	return size
}

// WriteSnapshot gathers all telemetry and writes the snapshot to the telemetry.ring file.
func WriteSnapshot(cfg *config.Config, store *memory.MemoryStore, logStream func() string, topQueries func(int) []memory.QueryStat, networkInfo func() NetworkStats) {
	ringMu.Lock()
	defer ringMu.Unlock()

	// Gather metrics
	cacheHit, cacheMiss, dbHit, dbMiss := store.GetTelemetry()
	metrics := store.GetMetrics()
	docs, docErr := store.DocCount()
	if docErr != nil {
		slog.Debug("failed to read document count for telemetry snapshot", "error", docErr)
	}

	lsm, vlog := store.GetDBSize()
	bleveSize := dirSize(filepath.Join(cfg.GetDBPath(), "search_index"))

	stats := StorageStats{
		LsmKB:  float64(lsm) / 1024,
		VlogKB: float64(vlog) / 1024,
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	cpuPercent, cpuErr := cpu.Percent(0, false)
	var cpuUsage float64
	if cpuErr != nil {
		slog.Debug("failed to read CPU percent for telemetry snapshot", "error", cpuErr)
	} else if len(cpuPercent) > 0 {
		cpuUsage = cpuPercent[0]
	}

	gcSweeps, gcPruned, searchLat, searchCount, rpcBytes, boundViolations := store.GetExtendedTelemetry()
	createOps, updateOps, mergeOps := store.GetWriteOps()
	batchProcessed, batchErrs := store.GetBatchHealth()

	avgSearchLat := int64(0)
	if searchCount > 0 {
		avgSearchLat = searchLat / int64(searchCount) //nolint:gosec // searchCount is a bounded operational counter
	}

	// Category distribution — cadence gate: run ListCategories() every 3rd cycle (30s).
	categoryTick++
	if categoryTick%3 == 0 {
		if cats, err := store.ListCategories(context.Background()); err == nil {
			lastCategories = topNCategories(cats, 10)
		}
	}

	// Resolve network info from the callback or use defaults.
	netBlock := NetworkStats{
		StdioConnected: true,
		HTTPPort:       0,
		HTTPClients:    map[string]string{},
		TotalClients:   1,
	}
	if networkInfo != nil {
		netBlock = networkInfo()
	}

	// Resolve top queries from the callback.
	var queries []memory.QueryStat
	if topQueries != nil {
		queries = topQueries(10)
	}

	uptime := int64(time.Since(StartTime).Seconds())
	h24, h7d, h30d := store.GetTTLHorizon(context.Background(), cfg.DefaultPurgeDays())
	primitives := store.GetPrimitiveMetrics(uptime)

	snapshot := Snapshot{
		Storage: stats,
		Bleve: BleveStats{
			Documents: docs,
			Drift:     store.DriftAlerts(),
			IndexSize: bleveSize,
		},
		Taxonomy: metrics.Namespaces,
		Analytics: AnalyticsStats{
			CacheHits:       cacheHit,
			CacheMisses:     cacheMiss,
			DBHits:          dbHit,
			DBMisses:        dbMiss,
			AvgRPCLatencyMs: avgSearchLat,
			RPCPayloadBytes: rpcBytes,
			Primitives:      primitives,
		},
		WriteOps: WriteOpsStats{
			Created: createOps,
			Updated: updateOps,
			Merged:  mergeOps,
		},
		BatchHealth: BatchHealthStats{
			EntriesProcessed: batchProcessed,
			Errors:           batchErrs,
		},
		Categories: lastCategories,
		MemoryGC: MemoryGCStats{
			Sweeps:      gcSweeps,
			PrunedNodes: gcPruned,
			Horizon24h:  h24,
			Horizon7d:   h7d,
			Horizon30d:  h30d,
		},
		Network: netBlock,
		Security: SecurityStats{
			BoundaryViolations: boundViolations,
		},
		Config: ConfigStats{
			DBPath:        cfg.GetDBPath(),
			Version:       cfg.Version,
			LogLevel:      "INFO",
			EnvGomemlimit: os.Getenv("GOMEMLIMIT"),
		},
		Runtime: RuntimeStats{
			MemoryMB:   m.Alloc / 1024 / 1024,
			Goroutines: runtime.NumGoroutine(),
			UptimeSec:  int64(time.Since(StartTime).Seconds()),
			NumGC:      m.NumGC,
			CPUUsage:   cpuUsage,
		},
		TopQueries: queries,
	}

	snapBytes, err := json.Marshal(snapshot)
	if err != nil {
		slog.Warn("failed to marshal telemetry snapshot", "error", err)
		return
	}
	logData := logStream()

	// Write atomically to telemetry.ring
	path := filepath.Join(cfg.GetDBPath(), "telemetry.ring")
	tmpPath := path + ".tmp"

	// Format: Single Line JSON \n Log Lines
	payload := fmt.Appendf(nil, "%s\n%s", string(snapBytes), logData)

	if err := os.WriteFile(tmpPath, payload, 0o600); err != nil {
		slog.Warn("failed to write telemetry snapshot temp file", "error", err)
		return
	}
	if err := os.Rename(tmpPath, path); err != nil {
		slog.Warn("failed to publish telemetry snapshot", "error", err)
	}
}

// topNCategories sorts a category map by count descending and returns the top N as a slice.
func topNCategories(cats map[string]int, n int) []CategoryEntry {
	type catEntry struct {
		Name  string
		Count int
	}
	entries := make([]catEntry, 0, len(cats))
	for k, v := range cats {
		entries = append(entries, catEntry{k, v})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Count > entries[j].Count
	})
	if len(entries) > n {
		entries = entries[:n]
	}
	result := make([]CategoryEntry, len(entries))
	for i, e := range entries {
		result[i] = CategoryEntry{Category: e.Name, Count: e.Count}
	}
	return result
}
