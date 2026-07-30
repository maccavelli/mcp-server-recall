// Package server provides functionality for the server subsystem.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/maccavelli/mcp-server-recall/internal/config"
	"github.com/maccavelli/mcp-server-recall/internal/harvest"
	"github.com/maccavelli/mcp-server-recall/internal/memory"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (rs *MCPRecallServer) handleHarvestStandards(ctx context.Context, req *mcp.CallToolRequest, args HarvestStandardsInput) (*mcp.CallToolResult, any, error) {
	return rs.handleHarvest(ctx, req, args.TargetPath, memory.DomainStandards)
}

func (rs *MCPRecallServer) handleHarvestProjects(ctx context.Context, req *mcp.CallToolRequest, args HarvestProjectsInput) (*mcp.CallToolResult, any, error) {
	return rs.handleHarvest(ctx, req, args.TargetPath, memory.DomainProjects)
}

func (rs *MCPRecallServer) handleHarvest(ctx context.Context, _ *mcp.CallToolRequest, targetPath string, targetDomain string) (*mcp.CallToolResult, any, error) {
	startHarvest := time.Now()

	resolvedPkg, err := harvest.ResolveSource(ctx, targetPath)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Resolver Error: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	var res *harvest.HarvestResult
	if strings.HasPrefix(resolvedPkg, "http://") || strings.HasPrefix(resolvedPkg, "https://") {
		slog.Info("[Perf] Start ScrapeWebDocument", "url", resolvedPkg)
		res, err = harvest.ScrapeWebDocument(ctx, resolvedPkg)
		slog.Info("[Perf] End ScrapeWebDocument", "dur_ms", time.Since(startHarvest).Milliseconds())
	} else {
		slog.Info("[Perf] Start engine.Run", "pkg", resolvedPkg)
		engine := harvest.NewEngine(rs.cfg.ExcludeDirs())
		res, err = engine.Run(ctx, resolvedPkg)
		slog.Info("[Perf] End engine.Run", "dur_ms", time.Since(startHarvest).Milliseconds())
	}
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Harvest Engine critical failure: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	// Maintenance Mode: CheckDrift
	tDrift := time.Now()
	drifted := rs.hasDrifted(ctx, targetDomain, resolvedPkg, res.Checksum)
	slog.Info("[Perf] hasDrifted check", "drifted", drifted, fieldDomain, targetDomain, "dur_ms", time.Since(tDrift).Milliseconds())

	if !drifted {
		return &mcp.CallToolResult{
			StructuredContent: map[string]any{
				fieldSummary: fmt.Sprintf("No API drift detected in %s domain. Package structurally identical to stored record.", targetDomain),
				fieldData: map[string]string{
					"checksum":  res.Checksum,
					"status":    "unchanged",
					fieldDomain: targetDomain,
				},
			},
		}, nil, nil
	}

	// We drift! Process the ingestion natively.
	tIngest := time.Now()
	stored, batchErrs := rs.ingestHarvestResult(ctx, resolvedPkg, res, targetDomain)
	slog.Info("[Perf] End ingestHarvestResult", "stored", stored, fieldDomain, targetDomain, "dur_ms", time.Since(tIngest).Milliseconds())

	msg := fmt.Sprintf("Harvest Update (Drift Detected) Complete [%s]. Old versions have been superseded.", targetDomain)

	return &mcp.CallToolResult{
		StructuredContent: map[string]any{
			fieldSummary: fmt.Sprintf("%s Extracted %d structural symbols.", msg, stored),
			fieldData: map[string]any{
				"symbols_harvested": len(res.Symbols),
				"batch_errors":      batchErrs,
				"checksum":          res.Checksum,
				"package":           resolvedPkg,
				fieldDomain:         targetDomain,
			},
		},
	}, nil, nil
}

func (rs *MCPRecallServer) hasDrifted(ctx context.Context, domain, pkg string, newChecksum string) bool {
	if rs.cfg.HarvestDisableDrift() {
		return true // Bypasses cache check through dynamic configuration
	}
	driftKey := fmt.Sprintf("pkg:%s:%s:CheckDrift", domain, pkg)
	oldChecksum, err := rs.store.Get(ctx, driftKey)
	if err == nil && oldChecksum.Category == "SysDrift" && oldChecksum.Content == newChecksum {
		return false
	}
	return true
}

func (rs *MCPRecallServer) ingestHarvestResult(ctx context.Context, resolvedPkg string, res *harvest.HarvestResult, targetDomain string) (int, []string) {
	batchCfg := rs.cfg.BatchSettings()
	driftKey := fmt.Sprintf("pkg:%s:%s:CheckDrift", targetDomain, resolvedPkg)
	var batch []memory.BatchEntry

	for _, sym := range res.Symbols {
		entry, err := buildSymbolEntry(sym, targetDomain)
		if err != nil {
			slog.Warn("Failed to marshal harvested symbol", "name", sym.Name, "error", err)
			continue
		}
		entry.Domain = targetDomain
		batch = append(batch, entry)
	}

	for pkg, pDoc := range res.PackageDocs {
		batch = append(batch, memory.BatchEntry{
			Key:      fmt.Sprintf("pkg:%s:PackageOverview", pkg),
			Value:    pDoc,
			Category: "PackageDoc",
			Tags:     []string{"packagedoc", fmt.Sprintf("source:%s", targetDomain)},
			Domain:   targetDomain,
		})
	}

	stored, batchErrs := rs.writeHarvestBatch(ctx, batch, batchCfg)
	if len(batchErrs) > 0 {
		return stored, batchErrs
	}

	// Write drift checksum ONLY after all other entries have successfully committed
	_, saveErr := rs.store.Save(ctx, "CheckDrift", driftKey, res.Checksum, "SysDrift", nil, targetDomain, 0.0)
	if saveErr != nil {
		batchErrs = append(batchErrs, fmt.Sprintf("Failed to store CheckDrift checksum: %v", saveErr))
	} else {
		stored++
	}

	return stored, batchErrs
}

// buildSymbolEntry creates a BatchEntry from a harvested symbol, including tags and domain detection.
func buildSymbolEntry(sym harvest.HarvestedSymbol, targetDomain string) (memory.BatchEntry, error) {
	key := fmt.Sprintf("pkg:%s:%s", sym.PkgPath, sym.Name)

	valBytes, err := json.MarshalIndent(sym, "", "  ")
	if err != nil {
		return memory.BatchEntry{}, err
	}

	tags := []string{"harvested", fmt.Sprintf("source:%s", targetDomain)}
	if mod := extractModuleName(sym.PkgPath); mod != "" {
		tags = append(tags, fmt.Sprintf("module:%s", mod))
	}
	if sym.SymbolType != "" {
		tags = append(tags, fmt.Sprintf("type:%s", sym.SymbolType))
	}
	for _, iface := range sym.Interfaces {
		tags = append(tags, fmt.Sprintf("implements:%s", iface))
	}
	if sym.Receiver != "" {
		tags = append(tags, fmt.Sprintf("receiver:%s", sym.Receiver))
	}
	for _, dep := range sym.Dependencies {
		tags = append(tags, fmt.Sprintf("depends_on:%s", dep))
	}
	tags = append(tags, detectDomainTags(sym.Doc)...)

	return memory.BatchEntry{
		Key:        key,
		Value:      string(valBytes),
		Category:   "HarvestedCode", //nolint:goconst // bypass
		Tags:       tags,
		SymbolName: sym.Name,
	}, nil
}

// extractModuleName derives a short module name from a Go package path for module-level tagging.
// External: github.com/blevesearch/bleve/v2/... → bleve
// Local: mcp-server-recall/internal/memory → mcp-server-recall
func extractModuleName(pkgPath string) string {
	parts := strings.Split(pkgPath, "/")
	if len(parts) == 0 {
		return ""
	}
	// External modules (github.com, gitlab.com, golang.org, etc.)
	if len(parts) >= 3 && strings.Contains(parts[0], ".") {
		name := parts[2]
		// If the third segment is a version (v2, v3), use the repo name
		if len(name) >= 2 && name[0] == 'v' && name[1] >= '0' && name[1] <= '9' {
			name = parts[1] // fallback to org name
		}
		return name
	}
	// Local modules: first path segment is the module name
	return parts[0]
}

// detectDomainTags scans documentation text for domain-indicating keywords.
func detectDomainTags(doc string) []string {
	docLower := strings.ToLower(doc)
	domains := map[string]string{
		harvestAuth:     harvestAuth,
		"security":      harvestAuth,
		harvestDatabase: harvestDatabase,
		"sql":           harvestDatabase,
		"api":           "api",
		"middleware":    "middleware",
		"architecture":  "architecture",
		"observability": "observability",
		"metrics":       "metrics",
		"telemetry":     "telemetry",
		"concurrency":   "concurrency",
		"network":       "network",
		"orchestrator":  "orchestrator",
		"design":        "design",
		harvestTest:     harvestTest,
	}
	var tags []string
	for kw, domain := range domains {
		if strings.Contains(docLower, kw) {
			tags = append(tags, fmt.Sprintf("domain:%s", domain))
		}
	}
	return tags
}

// writeHarvestBatch writes batch entries in chunks with optional throttling.
func (rs *MCPRecallServer) writeHarvestBatch(ctx context.Context, batch []memory.BatchEntry, batchCfg config.BatchConfig) (int, []string) {
	stored := 0
	var batchErrs []string

	chunkSize := batchCfg.HarvestChunkSize
	sleepMs := batchCfg.HarvestInterBatchSleepMs

	// Fast mode: when load_fast_writes_enabled=1, zero sleep and double chunks
	if batchCfg.LoadFastWritesEnabled == 1 {
		sleepMs = 0
		chunkSize *= 2
	}

	for i := 0; i < len(batch); i += chunkSize {
		end := min(i+chunkSize, len(batch))
		chunk := batch[i:end]
		count, errors, bErr := rs.store.SaveBatch(ctx, chunk)
		stored += count
		if bErr != nil {
			batchErrs = append(batchErrs, bErr.Error())
		}
		for _, e := range errors {
			batchErrs = append(batchErrs, fmt.Sprintf("Key %s: %v", e.Key, e.Error))
		}
		// Throttle between batches to prevent memtable saturation and system starvation
		if sleepMs > 0 {
			time.Sleep(time.Duration(sleepMs) * time.Millisecond)
		}
	}

	return stored, batchErrs
}
