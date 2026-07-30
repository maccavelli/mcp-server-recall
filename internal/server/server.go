// Package server provides functionality for the server subsystem.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"

	"github.com/maccavelli/mcplib"

	"github.com/maccavelli/mcp-server-recall/internal/config"
	"github.com/maccavelli/mcp-server-recall/internal/memory"
	"github.com/maccavelli/mcp-server-recall/internal/telemetry"
)

// MCPRecallServer defines the server that implements the mcp-server-recall tools.
type MCPRecallServer struct {
	mcpServer       *mcp.Server
	store           *memory.MemoryStore
	logs            *mcplib.LogBuffer
	cfg             *config.Config
	telemetryServer *telemetry.Server
	stopTelemetry   chan struct{} // Signals telemetry goroutine to stop
	startTime       time.Time     // Application start time for uptime
	closed          atomic.Bool   // Shutdown gate
}

// Close performs graceful shutdown of the server components.
func (rs *MCPRecallServer) Close() {
	if rs.closed.Swap(true) {
		return
	}
	close(rs.stopTelemetry)
	if rs.telemetryServer != nil {
		rs.telemetryServer.Close()
	}
}

// NewMCPRecallServer creates a new instance of the recall server.
func NewMCPRecallServer(ctx context.Context, cfg *config.Config, store *memory.MemoryStore, logs *mcplib.LogBuffer, logger *slog.Logger, topQueries func(int) []memory.QueryStat, networkInfo func() telemetry.NetworkStats) (*MCPRecallServer, error) {
	s := mcp.NewServer(
		&mcp.Implementation{
			Name:    cfg.Name(),
			Version: cfg.Version,
		},
		&mcp.ServerOptions{Logger: logger},
	)

	rs := &MCPRecallServer{
		mcpServer:     s,
		store:         store,
		logs:          logs,
		cfg:           cfg,
		startTime:     time.Now(),
		stopTelemetry: make(chan struct{}),
	}

	rs.registerTools()

	telemetry.StartTelemetryLoop(ctx, cfg, store, logs.String, topQueries, networkInfo)

	ts := telemetry.NewServer()
	if ts != nil {
		ts.Start()
	}
	rs.telemetryServer = ts

	go func() {
		ticker := time.NewTicker(telemetry.EmissionInterval)
		defer ticker.Stop()

		for {
			select {
			case <-rs.stopTelemetry:
				return
			case <-ticker.C:
			}

			if rs.telemetryServer == nil {
				continue
			}

			var m runtime.MemStats
			runtime.ReadMemStats(&m)

			cpuPercent, cpuErr := cpu.Percent(0, false)
			var cpuUsage float64
			if cpuErr != nil {
				slog.Debug("failed to read CPU percent for telemetry", "error", cpuErr)
			} else if len(cpuPercent) > 0 {
				cpuUsage = cpuPercent[0]
			}

			cacheHit, cacheMiss, dbHit, dbMiss := store.GetTelemetry()
			gcSweeps, _, sLat, sCount, rBytes, boundViolations := store.GetExtendedTelemetry()
			createOps, updateOps, mergeOps := store.GetWriteOps()

			searchCount := int64(sCount) //nolint:gosec // telemetry counters are bounded operational metrics
			avgSearchLat := float64(0)
			if searchCount > 0 {
				avgSearchLat = float64(sLat) / float64(searchCount)
			}

			stdioConn := false
			httpPort := 0
			totalClients := 1
			if networkInfo != nil {
				netBlock := networkInfo()
				stdioConn = netBlock.StdioConnected
				httpPort = netBlock.HTTPPort
				totalClients = netBlock.TotalClients
			}

			metrics := store.GetMetrics()

			rawPayload := telemetry.MetricPayloadPool.Get()
			payload, ok := rawPayload.(*telemetry.MetricPayload)
			if !ok {
				slog.Warn("telemetry payload pool returned unexpected type", "type", fmt.Sprintf("%T", rawPayload))
				continue
			}
			payload.MemoryMB = m.Alloc / 1024 / 1024
			payload.Goroutines = runtime.NumGoroutine()
			payload.UptimeSec = int64(time.Since(rs.startTime).Seconds())
			payload.NumGC = m.NumGC
			payload.CPUUsage = cpuUsage
			payload.CacheHits = int64(cacheHit)     //nolint:gosec // telemetry counters are bounded operational metrics
			payload.CacheMisses = int64(cacheMiss)  //nolint:gosec // telemetry counters are bounded operational metrics
			payload.DBHits = int64(dbHit)           //nolint:gosec // telemetry counters are bounded operational metrics
			payload.DBMisses = int64(dbMiss)        //nolint:gosec // telemetry counters are bounded operational metrics
			payload.RPCPayloadBytes = int64(rBytes) //nolint:gosec // telemetry counters are bounded operational metrics
			payload.AvgRPCLatencyMs = avgSearchLat
			payload.StdioConnected = stdioConn
			payload.HTTPPort = httpPort
			payload.TotalClients = totalClients
			payload.BleveDocs = metrics.BleveDocs
			maps.Copy(payload.Namespaces, metrics.Namespaces)
			payload.SearchQueries = sCount
			payload.CreateOps = createOps
			payload.UpdateOps = updateOps
			payload.MergeOps = mergeOps
			payload.GCSweeps = gcSweeps
			payload.BoundaryViolations = boundViolations

			rs.telemetryServer.Broadcast(payload)
		}
	}()

	return rs, nil
}

// toolCatalog returns the central registry of all MCP tools.

func (rs *MCPRecallServer) handleContextVacuum(ctx context.Context, req *mcp.CallToolRequest, args ContextVacuumInput) (*mcp.CallToolResult, any, error) {
	if args.FlattenThreshold <= 0 {
		args.FlattenThreshold = 1000
	}
	if args.Namespace == "" {
		args.Namespace = nsStandards
	}
	if args.DedupThreshold <= 0 {
		args.DedupThreshold = 0.7
	}

	var reports []*memory.VacuumReport
	var sessionMutated int

	// Namespace dispatch.
	switch args.Namespace {
	case nsMemories:
		report, err := rs.store.VacuumMemories(ctx, args.DedupThreshold, args.Category, args.ReportOnly)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error performing memory vacuum: %v", err)}},
				IsError: true,
			}, nil, nil
		}
		reports = append(reports, report)

	case nsStandards:
		report, err := rs.store.VacuumStandards(ctx, args.ReportOnly)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error performing standards vacuum: %v", err)}},
				IsError: true,
			}, nil, nil
		}
		reports = append(reports, report)

	case nsProjects:
		report, err := rs.store.VacuumProjects(ctx, args.ReportOnly)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error performing projects vacuum: %v", err)}},
				IsError: true,
			}, nil, nil
		}
		reports = append(reports, report)

	case "all": //nolint:goconst // bypass
		runSessionVacuum := func(ns string, days int) {
			m, err := rs.store.VacuumSessions(ctx, ns, args.TargetOutcome, args.FlattenThreshold, days)
			if err != nil {
				slog.Warn(fmt.Sprintf("%s vacuum portion failed during full vacuum", ns), "error", err)
			}
			sessionMutated += m
		}

		globalDays := args.DaysOld
		if globalDays <= 0 {
			globalDays = rs.cfg.DefaultPurgeDays()
		}

		// Run for all dynamically authorized namespaces except memories, standards, projects, ecosystem
		for _, ns := range rs.cfg.AuthorizedNamespaces() {
			if ns != nsMemories && ns != nsStandards && ns != nsProjects && ns != fieldEcosystem {
				runSessionVacuum(ns, globalDays)
			}
		}

		memReport, err := rs.store.VacuumMemories(ctx, args.DedupThreshold, args.Category, args.ReportOnly)
		if err != nil {
			slog.Warn("Memory vacuum portion failed during full vacuum", "error", err)
		} else {
			reports = append(reports, memReport)
		}

		stdReport, err := rs.store.VacuumStandards(ctx, args.ReportOnly)
		if err != nil {
			slog.Warn("Standards vacuum portion failed during full vacuum", "error", err)
		} else {
			reports = append(reports, stdReport)
		}

		projReport, err := rs.store.VacuumProjects(ctx, args.ReportOnly)
		if err != nil {
			slog.Warn("Projects vacuum portion failed during full vacuum", "error", err)
		} else {
			reports = append(reports, projReport)
		}

	default:
		authorized := slices.Contains(rs.cfg.AuthorizedNamespaces(), args.Namespace)
		if !authorized {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Invalid namespace: %q. Not in AuthorizedNamespaces.", args.Namespace)}},
				IsError: true,
			}, nil, nil
		}

		if args.DaysOld <= 0 {
			args.DaysOld = rs.cfg.DefaultPurgeDays()
		}
		mutated, err := rs.store.VacuumSessions(ctx, args.Namespace, args.TargetOutcome, args.FlattenThreshold, args.DaysOld)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error performing %s vacuum: %v", args.Namespace, err)}},
				IsError: true,
			}, nil, nil
		}
		sessionMutated = mutated
	}

	// Build structured result.
	result := map[string]any{
		"namespace": args.Namespace,
	}

	if sessionMutated > 0 || args.Namespace != nsMemories && args.Namespace != nsStandards && args.Namespace != nsProjects {
		result["sessions_pruned"] = sessionMutated
	}

	if len(reports) > 0 {
		var summaryParts []string
		for _, r := range reports {
			summaryParts = append(summaryParts, fmt.Sprintf("[%s] scanned=%d stale=%d duplicates=%d pruned=%d merged=%d",
				r.Namespace, r.TotalScanned, len(r.StaleEntries), len(r.DuplicateClusters), r.Pruned, r.Merged))
			result[r.Namespace+"_report"] = r
		}
		result[fieldSummary] = strings.Join(summaryParts, " | ")
	} else {
		result[fieldSummary] = fmt.Sprintf("Context vacuum completed: %d '%s' records semantic-pruned and tombstoned. Defragmentation constraints evaluated against Threshold %d. ValueLog GC triggered.",
			sessionMutated, args.TargetOutcome, args.FlattenThreshold)
	}

	return &mcp.CallToolResult{
		StructuredContent: result,
	}, nil, nil
}

// handleConsolidate has been removed — dedup is now inline in remember/batch_remember.

func (rs *MCPRecallServer) handleRemember(ctx context.Context, req *mcp.CallToolRequest, args RememberInput) (*mcp.CallToolResult, any, error) {
	hasEntries := args.Entries != nil && len(*args.Entries) > 0
	hasKey := args.Key != nil && *args.Key != ""
	hasValue := args.Value != nil && *args.Value != ""

	if hasEntries && (hasKey || hasValue) {
		return nil, nil, errors.New("cannot provide both single and batch fields")
	}

	if hasEntries {
		entries := *args.Entries
		for i := range entries {
			if strings.TrimSpace(entries[i].Key) == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: entry[%d] has an empty key", i)}}, IsError: true}, nil, nil
			}
			if strings.TrimSpace(entries[i].Value) == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: entry[%d] (key=%q) has an empty value", i, entries[i].Key)}}, IsError: true}, nil, nil
			}
			entries[i].Domain = memory.DomainMemories
		}

		stored, batchErrors, err := rs.store.SaveBatch(ctx, entries)
		if err != nil {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Batch save error: %v", err)}}, IsError: true}, nil, nil
		}

		summary := fmt.Sprintf("Batch save complete: %d stored, %d failed.", stored, len(batchErrors))
		return &mcp.CallToolResult{
			StructuredContent: map[string]any{
				fieldSummary: summary,
				fieldData: map[string]any{
					"stored": stored,
					"failed": len(batchErrors),
					"errors": batchErrors,
				},
			},
		}, nil, nil
	}

	if !hasKey || !hasValue {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "Error: key and value are required for single-record remember"}}, IsError: true}, nil, nil
	}

	valueVal := *args.Value
	if len(valueVal) > 15000000 {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "Error: Payload value exceeds maximum length bounds (15MB limit) preventing Memory OOM."}}, IsError: true}, nil, nil
	}

	threshold := rs.cfg.DedupThreshold()
	if args.DedupThreshold != nil && *args.DedupThreshold > 0 {
		threshold = *args.DedupThreshold
	}

	titleVal := mcplib.StringValue(args.Title)
	keyVal := *args.Key
	categoryVal := mcplib.StringValue(args.Category)
	var tagsVal []string
	if args.Tags != nil {
		tagsVal = *args.Tags
	}

	result, err := rs.store.Save(ctx, titleVal, keyVal, valueVal, categoryVal, tagsVal, memory.DomainMemories, threshold)
	if err != nil {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: %v", err)}}, IsError: true}, nil, nil
	}

	summary := fmt.Sprintf("Memory for '%s' %s.", result.Key, result.Action)
	data := map[string]any{
		fieldMessage:  summary,
		fieldAction:   result.Action,
		fieldTitle:    titleVal,
		fieldKey:      result.Key,
		fieldTags:     tagsVal,
		fieldCategory: categoryVal,
	}
	if result.MergedKey != "" {
		data["merged_with"] = result.MergedKey
	}

	return &mcp.CallToolResult{
		StructuredContent: map[string]any{
			fieldSummary: summary,
			fieldData:    data,
		},
	}, nil, nil
}

func (rs *MCPRecallServer) handleSaveToRecall(ctx context.Context, req *mcp.CallToolRequest, args SaveToRecallInput) (*mcp.CallToolResult, any, error) {
	if len(args.StateData) > 15000000 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: Session StateData exceeds maximum length bounds (15MB limit) preventing Memory OOM."}},
			IsError: true,
		}, nil, nil
	}

	tags := []string{"session"}
	if args.ProjectID != "" {
		tags = append(tags, fmt.Sprintf("project:%s", args.ProjectID))
	}
	if args.Outcome != "" {
		tags = append(tags, fmt.Sprintf("outcome:%s", args.Outcome))
	}
	if args.Model != "" {
		tags = append(tags, fmt.Sprintf("model:%s", args.Model))
	}
	if args.TraceContext != "" {
		tags = append(tags, fmt.Sprintf("trace:%s", args.TraceContext))
	}

	// Namespace-to-domain mapping and dynamic validation
	nsTitles := map[string]string{
		nsSessions:           "Session State",
		nsServerStatus:       "Server Status",
		nsDialecticHistory:   "Dialectic Archive",
		nsStandards:          "Standard",
		nsProjects:           "Project",
		fieldEcosystem:       "Ecosystem Entry",
		nsModernizerVerdicts: "Modernizer Verdict", // BUG-2
		nsModernizerTrust:    "Transform Trust",
		"madr_state":         "MADR Decision Record", //nolint:goconst // bypass
	}

	// Default to standards for backward compatibility
	ns := args.Namespace
	if ns == "" {
		ns = nsStandards
	}

	// Reject memories — must use remember/batch_remember
	if ns == nsMemories {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Invalid namespace: \"memories\". Use the 'remember' or 'batch_remember' tool for memory writes."}},
			IsError: true,
		}, nil, nil
	}

	domain := rs.namespaceToDomain(ns)
	if domain == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Invalid namespace: %q. Not in AuthorizedNamespaces.", ns)}},
			IsError: true,
		}, nil, nil
	}

	// Dynamic Schema Enforcement
	if schema, exists := rs.cfg.NamespaceSchemas()[ns]; exists && len(schema.RequiredKeys) > 0 {
		var missing []string
		for _, key := range schema.RequiredKeys {
			if !strings.Contains(args.StateData, key) {
				missing = append(missing, key)
			}
		}
		if len(missing) > 0 {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("[VALIDATION REQUIRED] Namespace %q schema requires the following missing keys in StateData payload: %s", ns, strings.Join(missing, ", "))}},
				IsError: true,
			}, nil, nil
		}
	}

	title, ok := nsTitles[ns]
	if !ok {
		title = ns
	}

	// Key generation: sessions/server_status use 5-part matrix; others use client-provided or auto-generated
	var key string
	if args.Key != "" {
		key = args.Key
	} else if ns == nsSessions || ns == nsServerStatus || ns == fieldEcosystem {
		key = fmt.Sprintf("%s:session:%s:%s:%s", args.ServerID, args.ProjectID, args.Outcome, mcplib.StringValue(args.SessionID))
	} else {
		key = fmt.Sprintf("%s:%s:%d", args.ServerID, ns, time.Now().UnixNano())
	}

	// Category resolution: use explicit category if provided, fall back to ServerID for backward compatibility.
	category := args.ServerID
	if args.Category != "" {
		category = args.Category
	}

	result, err := rs.store.Save(ctx, title, key, args.StateData, category, tags, domain, 0.0)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	summary := fmt.Sprintf("State for '%s' saved to %s namespace.", key, domain)
	return &mcp.CallToolResult{
		StructuredContent: map[string]any{
			fieldSummary: summary,
			fieldData: map[string]any{
				fieldKey:      key,
				fieldServerID: args.ServerID,
				"session_id":  mcplib.StringValue(args.SessionID),
				"namespace":   domain,
				fieldAction:   result.Action,
			},
		},
	}, nil, nil
}

func (rs *MCPRecallServer) handleRecall(ctx context.Context, req *mcp.CallToolRequest, args RecallInput) (*mcp.CallToolResult, any, error) {
	var key string
	if args.Key != nil {
		key = *args.Key
	}
	var keys []string
	if args.Keys != nil {
		keys = *args.Keys
	}
	var count int
	if args.Count != nil {
		count = *args.Count
	}

	hasKey := key != ""
	hasKeys := len(keys) > 0
	hasCount := count > 0

	if (hasKey && hasKeys) || (hasKey && hasCount) || (hasKeys && hasCount) {
		return nil, nil, errors.New("cannot provide more than one of 'key', 'keys', or 'count' simultaneously")
	}

	if hasKeys {
		found, missing, err := rs.store.GetBatch(ctx, keys)
		if err != nil {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Batch recall error: %v", err)}}, IsError: true}, nil, nil
		}
		for k, rec := range found {
			if rec.Domain != memory.DomainMemories {
				delete(found, k)
				missing = append(missing, k)
			}
		}
		summary := fmt.Sprintf("Batch recall complete: %d found, %d missing.", len(found), len(missing))
		return &mcp.CallToolResult{
			StructuredContent: map[string]any{
				fieldSummary: summary,
				fieldData: map[string]any{
					fieldFound:   len(found),
					fieldMissing: missing,
					fieldEntries: found,
				},
			},
		}, nil, nil
	}

	if hasCount || (!hasKey && !hasKeys) {
		if count <= 0 {
			count = 10
		}
		results, err := rs.store.GetRecent(ctx, count)
		if err != nil {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: %v", err)}}, IsError: true}, nil, nil
		}
		return rs.formatResults("Recent Context Memories", req, results)
	}

	rec, err := rs.store.Get(ctx, key)
	if err != nil {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: %v", err)}}, IsError: true}, nil, nil
	}

	if rec.Domain != memory.DomainMemories {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Key '%s' belongs to the %s domain. Use 'get_sessions' or 'get_standards' instead.", key, rec.Domain)}}, IsError: true}, nil, nil
	}

	summary := fmt.Sprintf("Retrieved memory: %s", key)
	return &mcp.CallToolResult{
		StructuredContent: map[string]any{
			fieldSummary: summary,
			fieldData:    rec,
		},
	}, nil, nil
}

func (rs *MCPRecallServer) handleSearch(ctx context.Context, req *mcp.CallToolRequest, args SearchMemoriesInput) (*mcp.CallToolResult, any, error) {
	if args.Limit <= 0 {
		args.Limit = rs.cfg.DefaultPagination()
	}

	start := time.Now()
	resultsSeq, err := rs.store.Search(ctx, args.Query, args.Tag, args.Limit)
	elapsed := time.Since(start)

	// Update latency metrics
	rs.store.RecordSearchTelemetry(args.Query, elapsed.Milliseconds())

	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	var results []*memory.SearchResult
	for r := range resultsSeq {
		results = append(results, r)
	}

	return rs.formatResults(fmt.Sprintf("Search Results for '%s'", args.Query), req, results)
}

func (rs *MCPRecallServer) handleSearchSessions(ctx context.Context, req *mcp.CallToolRequest, args SearchSessionsInput) (*mcp.CallToolResult, any, error) {
	if args.Limit <= 0 {
		args.Limit = rs.cfg.DefaultPagination()
	}

	start := time.Now()
	resultsSeq, err := rs.store.SearchSessions(ctx, args.Domain, args.Query, args.ProjectID, args.ServerID, args.Outcome, args.TraceContext, args.Limit)
	elapsed := time.Since(start)

	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	// Update latency metrics only on successful searches.
	rs.store.RecordSearchTelemetry(args.Query, elapsed.Milliseconds())

	var results []*memory.SearchResult
	for r := range resultsSeq {
		results = append(results, r)
	}

	return rs.formatResults(fmt.Sprintf("Session Search Results for '%s'", args.Query), req, results)
}

func (rs *MCPRecallServer) handleList(ctx context.Context, req *mcp.CallToolRequest, _ ListMemoriesInput) (*mcp.CallToolResult, any, error) {
	keysSeq, err := rs.store.ListKeys(ctx)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	var keys []*memory.SearchResult
	for k := range keysSeq {
		keys = append(keys, k)
	}

	return rs.formatResults("Knowledge Index", req, keys)
}

func (rs *MCPRecallServer) handleListSessions(ctx context.Context, req *mcp.CallToolRequest, args ListSessionsInput) (*mcp.CallToolResult, any, error) {
	// Default limit to 50 to prevent payload explosion.
	if args.Limit <= 0 {
		args.Limit = 50
	}

	// Dispatch structured query constraints to the DB layer instead of pulling purely domain objects
	sessionsSeq, err := rs.store.ListSessions(ctx, args.Domain, args.ProjectID, args.ServerID, args.Outcome, args.TraceContext, args.Limit)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	var sessions []*memory.SearchResult
	for s := range sessionsSeq {
		sessions = append(sessions, s)
	}

	// Truncate content if requested to prevent payload overflow during aggregation.
	const maxContentLen = 32768 // 32KB
	if args.TruncateContent {
		for _, s := range sessions {
			if s.Record != nil && len(s.Record.Content) > maxContentLen {
				s.Record.Content = s.Record.Content[:maxContentLen]
				s.IsTruncated = true
			}
		}
	}

	return rs.formatResults("Analytic Session Dataset", req, sessions)
}

func (rs *MCPRecallServer) isNamespaceAuthorized(ns string) bool {
	return slices.Contains(rs.cfg.AuthorizedNamespaces(), ns) || rs.namespaceToDomain(ns) != ""
}

func (rs *MCPRecallServer) handleGetSessions(ctx context.Context, _ *mcp.CallToolRequest, args GetSessionsInput) (*mcp.CallToolResult, any, error) {
	targetDomain := args.Domain
	if targetDomain == "" {
		targetDomain = memory.DomainSessions
	}

	if args.Domain != "" && !rs.isNamespaceAuthorized(args.Domain) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: namespace '%s' is not supported by session handlers", args.Domain)}},
			IsError: true,
		}, nil, nil
	}
	// Direct key lookup takes precedence.
	if args.Key != "" {
		rec, err := rs.store.Get(ctx, args.Key)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Session not found: %v", err)}},
				IsError: true,
			}, nil, nil
		}
		if args.Domain != "" && rec.Domain != args.Domain {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Key '%s' exists but belongs to domain '%s', not requested domain '%s'.", args.Key, rec.Domain, args.Domain)}},
				IsError: true,
			}, nil, nil
		} else if args.Domain == "" && !rs.isNamespaceAuthorized(rec.Domain) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Key '%s' is not a session/status record (domain: %s). Use 'recall' for memories.", args.Key, rec.Domain)}},
				IsError: true,
			}, nil, nil
		}
		return &mcp.CallToolResult{
			StructuredContent: map[string]any{
				fieldSummary: fmt.Sprintf("Session '%s' retrieved.", args.Key),
				fieldData: map[string]any{
					fieldKey:       args.Key,
					fieldServerID:  rec.Category,
					fieldContent:   rec.Content,
					fieldTags:      rec.Tags,
					fieldCreatedAt: rec.CreatedAt,
					fieldUpdatedAt: rec.UpdatedAt,
				},
			},
		}, nil, nil
	}

	// Targeted suffix-match scan using session_id across the requested domain.
	// Uses domain index prefix scan with inline suffix filtering to avoid
	// materializing all sessions (was O(N) on the entire namespace).
	if args.SessionID != nil && *args.SessionID != "" {
		suffix := ":" + *args.SessionID
		bestMatch, err := rs.store.FindSessionBySuffix(ctx, targetDomain, suffix)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error scanning sessions: %v", err)}},
				IsError: true,
			}, nil, nil
		}

		if bestMatch == nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("No session found matching session_id '%s'.", *args.SessionID)}},
				IsError: true,
			}, nil, nil
		}

		return &mcp.CallToolResult{
			StructuredContent: map[string]any{
				fieldSummary: fmt.Sprintf("Session '%s' retrieved via session_id match.", bestMatch.Key),
				fieldData: map[string]any{
					fieldKey:       bestMatch.Key,
					fieldServerID:  bestMatch.Record.Category,
					fieldContent:   bestMatch.Record.Content,
					fieldTags:      bestMatch.Record.Tags,
					fieldCreatedAt: bestMatch.Record.CreatedAt,
					fieldUpdatedAt: bestMatch.Record.UpdatedAt,
				},
			},
		}, nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "Either 'key' or 'session_id' must be provided."}},
		IsError: true,
	}, nil, nil
}

func (rs *MCPRecallServer) handleGetMetrics(ctx context.Context, _ *mcp.CallToolRequest, args GetMetricsInput) (*mcp.CallToolResult, any, error) {
	// Database Entry Stats
	count, size, err := rs.store.GetStats()
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error fetching DB stats: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	metrics := rs.store.GetMetrics()

	// Wait on SWR/BadgerDB Custom Counters
	cacheHits, cacheMisses, dbHits, dbMisses := rs.store.GetTelemetry()
	gcSweeps, gcPruned, sLat, sCount, rpcBytes, boundViolations := rs.store.GetExtendedTelemetry()
	createOps, updateOps, mergeOps := rs.store.GetWriteOps()
	h24, h7d, h30d := rs.store.GetTTLHorizon(ctx, rs.cfg.DefaultPurgeDays())
	topQueries := rs.store.GetTopQueries(10)
	docs, docErr := rs.store.DocCount()
	if docErr != nil {
		slog.Warn("failed to read document count for metrics", "error", docErr)
	}

	// Gather gopsutil metrics natively
	vMem, memErr := mem.VirtualMemory()
	if memErr != nil {
		slog.Warn("Failed to read virtual memory stats", "error", memErr)
	}
	cpuPct, cpuErr := cpu.Percent(0, false)
	if cpuErr != nil {
		slog.Warn("Failed to read CPU stats", "error", cpuErr)
	}
	hInfo, hostErr := host.Info()
	if hostErr != nil {
		slog.Warn("Failed to read host info", "error", hostErr)
	}

	cpuUsage := 0.0
	if len(cpuPct) > 0 {
		cpuUsage = cpuPct[0]
	}

	// Safe extraction of vMem fields (nil-guarded for containerized environments).
	var memTotalMB, memUsedPct, memAllocMB float64
	if vMem != nil {
		memTotalMB = float64(vMem.Total) / 1024 / 1024
		memUsedPct = vMem.UsedPercent
		memAllocMB = float64(vMem.Used) / 1024 / 1024
	}

	// Runtime and Application level
	appUptime := time.Since(rs.startTime)
	sysUptime := time.Duration(0)
	if hInfo != nil {
		sysUptime = time.Duration(hInfo.Uptime) * time.Second //nolint:gosec // host uptime is a bounded OS metric
	}

	primitives := rs.store.GetPrimitiveMetrics(int64(appUptime.Seconds()))

	avgLatency := float64(0)
	// We prefer the extended telemetry values over rs.searchCount.Load() for consistency
	if sCount > 0 {
		avgLatency = float64(sLat) / float64(sCount)
	}

	summary := fmt.Sprintf("System metrics retrieved. App Uptime: %s, CPU: %.2f%%, Mem Used: %.2f%%",
		appUptime.Round(time.Second), cpuUsage, memUsedPct)

	data := map[string]any{
		"system": map[string]any{
			"app_uptime":      appUptime.String(),
			"host_uptime_sec": sysUptime.Seconds(),
			"goroutines":      runtime.NumGoroutine(),
			"cpus_available":  runtime.NumCPU(),
			"cpu_usage_pct":   cpuUsage,
			"memory_total_mb": memTotalMB,
			"memory_used_pct": memUsedPct,
			"memory_alloc_mb": memAllocMB,
		},
		"storage": map[string]any{
			"db_entries":            count,
			"namespaces":            metrics.Namespaces,
			"size_formatted":        fmt.Sprintf("%.2f KB", float64(size)/1024),
			"db_hits":               dbHits,
			"db_misses":             dbMisses,
			"cache_hits":            cacheHits,
			"cache_misses":          cacheMisses,
			"index_drift_alerts":    rs.store.DriftAlerts(),
			"avg_search_latency_ms": fmt.Sprintf("%.2fms", avgLatency),
		},
		"bleve": map[string]any{
			"documents": docs,
		},
		"write_ops": map[string]any{
			"created": createOps,
			"updated": updateOps,
			"merged":  mergeOps,
		},
		"memory_gc": map[string]any{
			"sweeps":       gcSweeps,
			"pruned_nodes": gcPruned,
			"horizon_24h":  h24,
			"horizon_7d":   h7d,
			"horizon_30d":  h30d,
		},
		"security": map[string]any{
			"boundary_violations": boundViolations,
		},
		"analytics": map[string]any{
			"search_queries":    sCount,
			"rpc_payload_bytes": rpcBytes,
			"primitives":        primitives,
		},
		"top_queries": topQueries,
	}

	// Build readable text for proxy/Content consumers.
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Recall Metrics\n\n%s\n\n", summary)
	sb.WriteString("## System\n")
	fmt.Fprintf(&sb, "- App Uptime: %s\n", appUptime.Round(time.Second))
	fmt.Fprintf(&sb, "- Host Uptime: %.0fs\n", sysUptime.Seconds())
	fmt.Fprintf(&sb, "- Goroutines: %d\n", runtime.NumGoroutine())
	fmt.Fprintf(&sb, "- CPUs: %d\n", runtime.NumCPU())
	fmt.Fprintf(&sb, "- CPU Usage: %.2f%%\n", cpuUsage)
	fmt.Fprintf(&sb, "- Memory Total: %.0f MB\n", memTotalMB)
	fmt.Fprintf(&sb, "- Memory Used: %.2f%%\n", memUsedPct)
	fmt.Fprintf(&sb, "- Memory Alloc: %.0f MB\n", memAllocMB)
	sb.WriteString("\n## Storage & IO\n")
	fmt.Fprintf(&sb, "- DB Entries: %d\n", count)
	for domain, count := range metrics.Namespaces {
		fmt.Fprintf(&sb, "- %s: %d\n", domain, count)
	}
	fmt.Fprintf(&sb, "- Size: %.2f KB\n", float64(size)/1024)
	fmt.Fprintf(&sb, "- DB Hits: %d | Misses: %d\n", dbHits, dbMisses)
	fmt.Fprintf(&sb, "- Cache Hits: %d | Misses: %d\n", cacheHits, cacheMisses)
	fmt.Fprintf(&sb, "- Write Ops: %d created, %d updated, %d merged\n", createOps, updateOps, mergeOps)
	fmt.Fprintf(&sb, "- GC Sweeps: %d (Pruned: %d)\n", gcSweeps, gcPruned)
	fmt.Fprintf(&sb, "- Boundary Violations: %d\n", boundViolations)
	sb.WriteString("\n## Search Engine\n")
	fmt.Fprintf(&sb, "- Indexed Documents: %d\n", docs)
	fmt.Fprintf(&sb, "- Index Drift Alerts: %d\n", rs.store.DriftAlerts())
	fmt.Fprintf(&sb, "- Total Search Queries: %d\n", sCount)
	fmt.Fprintf(&sb, "- Avg Search Latency: %.2fms\n", avgLatency)

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: sb.String()}},
		StructuredContent: map[string]any{
			fieldSummary: summary,
			fieldData:    data,
		},
	}, nil, nil
}

func (rs *MCPRecallServer) handleForget(ctx context.Context, req *mcp.CallToolRequest, args ForgetInput) (*mcp.CallToolResult, any, error) {
	if args.Key != "" && len(args.Keys) > 0 {
		return nil, nil, errors.New("cannot provide both single and batch fields")
	}

	if len(args.Keys) > 0 {
		if err := rs.store.BatchDelete(ctx, args.Keys); err != nil {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: %v", err)}}, IsError: true}, nil, nil
		}
		summary := fmt.Sprintf("Batch forget complete: %d records removed.", len(args.Keys))
		return &mcp.CallToolResult{
			StructuredContent: map[string]any{
				fieldSummary: summary,
				fieldData: map[string]any{
					fieldMessage: summary,
					"keys":       args.Keys,
				},
			},
		}, nil, nil
	}

	if args.Key == "" {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "Error: key or keys are required for forget"}}, IsError: true}, nil, nil
	}

	rec, getErr := rs.store.Get(ctx, args.Key)
	if getErr != nil {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: key %q not found: %v", args.Key, getErr)}}, IsError: true}, nil, nil
	}
	if rec.Domain != memory.DomainMemories {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: key %q belongs to the %s domain. Use the 'delete' tool with the correct namespace instead.", args.Key, rec.Domain)}}, IsError: true}, nil, nil
	}

	if err := rs.store.Delete(ctx, args.Key); err != nil {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: %v", err)}}, IsError: true}, nil, nil
	}

	summary := fmt.Sprintf("Memory for '%s' forgotten.", args.Key)
	return &mcp.CallToolResult{
		StructuredContent: map[string]any{
			fieldSummary: summary,
			fieldData: map[string]any{
				fieldMessage: summary,
				fieldKey:     args.Key,
			},
		},
	}, nil, nil
}

func (rs *MCPRecallServer) handleListCategories(ctx context.Context, req *mcp.CallToolRequest, _ ListCategoriesInput) (*mcp.CallToolResult, any, error) {
	categories, err := rs.store.ListCategories(ctx)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error fetching categories: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	return rs.formatResults("Memory Categories", req, categories)
}

func (rs *MCPRecallServer) formatResults(title string, req *mcp.CallToolRequest, results any) (*mcp.CallToolResult, any, error) {
	var summary string

	var artifactPath string
	if req != nil && req.Params != nil && len(req.Params.Arguments) > 0 {
		var ap struct {
			ArtifactPath string `json:"artifact_path,omitempty,omitzero"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &ap); err == nil {
			artifactPath = strings.TrimSpace(ap.ArtifactPath)
		}
	}

	// Try to determine size if it's a slice
	count := 0
	switch v := results.(type) {
	case []*memory.SearchResult:
		count = len(v)
	case []string:
		count = len(v)
	case []any:
	case map[string]int:
		count = len(v)
	}

	res := &mcp.CallToolResult{}

	if count == 0 {
		summary = fmt.Sprintf("%s: No results found.", title)
		res.StructuredContent = map[string]any{
			fieldSummary: summary,
			fieldData: map[string]any{
				fieldMessage: "No matches found.",
			},
		}
	} else {
		summary = fmt.Sprintf("%s: Found %d entries.", title, count)
		res.StructuredContent = map[string]any{
			fieldSummary: summary,
			fieldData: map[string]any{
				fieldTitle:   title,
				fieldCount:   count,
				fieldEntries: results,
			},
		}
	}

	if rawJSON, err := json.MarshalIndent(res.StructuredContent, "", "  "); err == nil {
		rs.store.RecordRPCBytes(uint64(len(rawJSON)))
		if artifactPath != "" {
			if err := os.MkdirAll(filepath.Dir(artifactPath), 0o750); err != nil {
				return nil, nil, fmt.Errorf("failed to create artifact directory: %w", err)
			}
			if err := os.WriteFile(artifactPath, rawJSON, 0o600); err != nil {
				return nil, nil, fmt.Errorf("failed to write artifact: %w", err)
			}
			res.Content = []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Artifact written to: %s", artifactPath)}}
			return res, nil, nil
		}
		res.Content = []mcp.Content{&mcp.TextContent{Text: string(rawJSON)}}
	}

	return res, nil, nil
}

// Serve initializes the transport and starts serving the MCP protocol.
func (rs *MCPRecallServer) Serve(ctx context.Context, stdout io.WriteCloser, reader io.ReadCloser) error {
	slog.Info("Serving MCP-Recall on pure IOTransport")
	t := &mcp.IOTransport{
		Reader: reader,
		Writer: stdout,
	}
	_, err := rs.mcpServer.Connect(ctx, t, nil)
	return err
}

func (rs *MCPRecallServer) isImportExportPathAllowed(targetPath string) (bool, error) {
	allowedExportDir, err := filepath.Abs(rs.cfg.ExportDir())
	if err != nil {
		return false, fmt.Errorf("resolve export directory: %w", err)
	}
	allowedCacheDir, cacheErr := os.UserCacheDir()
	if cacheErr != nil {
		return false, fmt.Errorf("resolve user cache directory: %w", cacheErr)
	}
	return isSubDir(allowedExportDir, targetPath) ||
		(allowedCacheDir != "" && isSubDir(allowedCacheDir, targetPath)), nil
}

func isSubDir(parent, child string) bool {
	parent = filepath.Clean(parent)
	child = filepath.Clean(child)
	if parent == child {
		return true
	}
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..") && rel != "."
}

func (rs *MCPRecallServer) handleExportMemories(ctx context.Context, req *mcp.CallToolRequest, args ExportMemoriesInput) (*mcp.CallToolResult, any, error) {
	fname := args.Filename
	if fname == "" {
		fname = filepath.Join(rs.cfg.ExportDir(), fmt.Sprintf("recall_export_%s.jsonl", time.Now().Format("20060102_150405")))
	}

	exportPath, err := filepath.Abs(fname)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Failed to resolve absolute path: %v", err)}},
		}, nil, nil
	}

	isAllowed, sandboxErr := rs.isImportExportPathAllowed(exportPath)
	if sandboxErr != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: sandboxErr.Error()}},
		}, nil, sandboxErr
	}
	if !isAllowed {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Path sandboxing violation: %s is outside allowed export directories (ExportDir or UserCacheDir)", exportPath)}},
		}, nil, nil
	}

	count, err := rs.store.ExportJSONL(ctx, exportPath, "", nil)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Failed to export: %v", err)}},
		}, nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Successfully exported %d records to %s", count, exportPath)}},
	}, nil, nil
}

func (rs *MCPRecallServer) handleImportMemories(ctx context.Context, req *mcp.CallToolRequest, args ImportMemoriesInput) (*mcp.CallToolResult, any, error) {
	if args.Filename == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: filename is strictly required for import."}},
		}, nil, nil
	}

	importPath, err := filepath.Abs(args.Filename)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Failed to resolve absolute path: %v", err)}},
		}, nil, nil
	}

	isAllowed, sandboxErr := rs.isImportExportPathAllowed(importPath)
	if sandboxErr != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: sandboxErr.Error()}},
		}, nil, sandboxErr
	}
	if !isAllowed {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Path sandboxing violation: %s is outside allowed export directories (ExportDir or UserCacheDir)", importPath)}},
		}, nil, nil
	}

	count, errList, err := rs.store.ImportJSONL(ctx, importPath, "")
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Catastrophic error during import: %v. %d records succeeded.", err, count)}},
		}, nil, nil
	}

	if len(errList) > 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Partially imported %d records but encountered %d errors (e.g. %v).", count, len(errList), errList[0])}},
		}, nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Successfully imported %d records from %s", count, importPath)}},
	}, nil, nil
}

func (rs *MCPRecallServer) handleReloadCache(ctx context.Context, _ *mcp.CallToolRequest, args ReloadCacheInput) (*mcp.CallToolResult, any, error) {
	if rs.store == nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: store not initialized"}},
		}, nil, nil
	}

	if err := rs.store.SyncSearchIndex(ctx); err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Failed to reload cache: %v", err)}},
		}, nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "Search cache successfully re-synchronized with source of truth."}},
	}, nil, nil
}
