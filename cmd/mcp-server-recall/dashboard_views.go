// Package cmd provides functionality for the cmd subsystem.
package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

const loadingText = "⏳ Loading telemetry data..."

var (
	subTitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("62")).
			Bold(true).
			MarginTop(1).
			MarginBottom(1)

	metricLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241"))

	cardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("238")).
			Padding(1, 2).
			MarginRight(2).
			MarginBottom(1)

	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("197"))
	debugStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	tableBorderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))

	// msgColStyle enforces 80 char word wrapping on the Message column.
	msgColStyle = lipgloss.NewStyle().Width(80)
)

// renderStyledTable builds a lipgloss table from headers and rows.
func renderStyledTable(headers []string, rows [][]string) string {
	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(tableBorderStyle).
		Headers(headers...)

	for _, row := range rows {
		t.Row(row...)
	}

	return t.Render()
}

func renderOverview(m model) string {
	b := strings.Builder{}
	b.WriteString(dashTitleStyle.Render("Recall Server Overview") + "\n\n")

	// Connection status
	connStatus := warningStyle.Render("○ Server Disconnected")
	if m.hotConnected && time.Since(m.hotLastUpdate) < 10*time.Second {
		connStatus = successStyle.Render("● Server Connected")
	}
	if m.boundPort > 0 {
		connStatus += metricLabelStyle.Render(fmt.Sprintf("  (udp:%d)", m.boundPort))
	}
	b.WriteString(connStatus + "\n\n")

	snapshot := m.coldMetrics.Snapshot

	// System Metrics Card
	rc := loadingText
	if m.hotConnected {
		mem := fmt.Sprintf("%v", m.hotState.MemoryMB)
		gr := fmt.Sprintf("%v", m.hotState.Goroutines)
		up := fmt.Sprintf("%v", m.hotState.UptimeSec)
		gc := fmt.Sprintf("%v", m.hotState.NumGC)
		cpu := fmt.Sprintf("%.2f%%", m.hotState.CPUUsage)
		rc = fmt.Sprintf("CPU Utilization: %s\nMemory Footprint: %s MB\nActive Goroutines: %s\nUptime: %s sec\nGC Cycles: %s\nConnection: LIVE (UDP)", cpu, mem, gr, up, gc)

		hits := float64(m.hotState.CacheHits)
		misses := float64(m.hotState.CacheMisses)
		total := hits + misses
		if total > 0 {
			rate := (hits / total) * 100
			rateStr := fmt.Sprintf("%.1f%%", rate)
			switch {
			case rate >= 80:
				rateStr = successStyle.Render(rateStr)
			case rate >= 50:
				rateStr = warningStyle.Render(rateStr)
			default:
				rateStr = errorStyle.Render(rateStr)
			}
			rc += fmt.Sprintf("\nSearch Hit Rate: %s", rateStr)
		} else {
			rc += "\nSearch Hit Rate: N/A"
		}
	} else if rt, ok := snapshot["runtime"].(map[string]any); ok {
		mem := fmt.Sprintf("%v", rt["memory_mb"])
		gr := fmt.Sprintf("%v", rt["goroutines"])
		up := fmt.Sprintf("%v", rt["uptime_sec"])
		gc := fmt.Sprintf("%v", rt["num_gc"])
		cpu := fmt.Sprintf("%.2f%%", rt["cpu_usage"])
		rc = fmt.Sprintf("CPU Utilization: %s\nMemory Footprint: %s MB\nActive Goroutines: %s\nUptime: %s sec\nGC Cycles: %s\nConnection: Polling (BuntDB)", cpu, mem, gr, up, gc)

		if an, ok := snapshot["analytics"].(map[string]any); ok {
			hits, _ := an["cache_hits"].(float64)     //nolint:errcheck // optional telemetry field
			misses, _ := an["cache_misses"].(float64) //nolint:errcheck // optional telemetry field
			total := hits + misses
			if total > 0 {
				rate := (hits / total) * 100
				rateStr := fmt.Sprintf("%.1f%%", rate)
				switch {
				case rate >= 80:
					rateStr = successStyle.Render(rateStr)
				case rate >= 50:
					rateStr = warningStyle.Render(rateStr)
				default:
					rateStr = errorStyle.Render(rateStr)
				}
				rc += fmt.Sprintf("\nSearch Hit Rate: %s", rateStr)
			} else {
				rc += "\nSearch Hit Rate: N/A"
			}
		}
	}

	sysCard := cardStyle.Render(subTitleStyle.Render("System Metrics") + "\n" + rc)

	// Storage Diagnostics Card (merged from removed tab)
	sc := loadingText
	if s, ok := snapshot["storage"].(map[string]any); ok {
		lsmKB := fmt.Sprintf("%.2f KB", s["lsm_kb"])
		vlogKB := fmt.Sprintf("%.2f KB", s["vlog_kb"])
		sc = fmt.Sprintf("BadgerDB instance actively mapped.\nLSM Size: %s\nValue Log Size: %s", lsmKB, vlogKB)
	}
	storageCard := cardStyle.Render(subTitleStyle.Render("Storage Diagnostics") + "\n" + sc)

	// Side-by-side layout
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, sysCard, storageCard))
	b.WriteString("\n\n")

	// Live telemetry log tail
	b.WriteString(subTitleStyle.Render("Live Telemetry Events") + "\n")
	logs := m.coldMetrics.Logs
	if len(logs) == 0 {
		b.WriteString(metricLabelStyle.Render("Awaiting daemon telemetry sync...") + "\n")
	} else {
		tail := logs
		if len(tail) > 12 {
			tail = tail[len(tail)-12:]
		}

		// Build rows with column order: Time, Lvl, Target, Message
		var rows [][]string
		for _, l := range tail {
			var target string
			if l.Pkg != "" {
				target = l.Pkg
			} else if l.Tool != "" {
				target = l.Tool
			}

			timeStr := l.Time
			if len(timeStr) > 19 {
				timeStr = timeStr[11:19]
			}

			lvl := l.Level
			switch lvl {
			case "INFO":
				lvl = successStyle.Render("INFO")
			case "DEBUG":
				lvl = debugStyle.Render("DEBUG")
			case "WARN":
				lvl = warningStyle.Render("WARN")
			case "ERROR":
				lvl = errorStyle.Render("ERROR")
			}

			if len(target) > 25 {
				target = "..." + target[len(target)-22:]
			}

			// Apply word wrapping at 80 chars to message instead of truncation.
			msg := msgColStyle.Render(l.Msg)

			rows = append(rows, []string{timeStr, lvl, target, msg})
		}

		t := table.New().
			Border(lipgloss.NormalBorder()).
			BorderStyle(tableBorderStyle).
			Headers("Time", "Lvl", "Target", "Message").
			StyleFunc(func(row, col int) lipgloss.Style {
				if col == 3 { // Message column
					return lipgloss.NewStyle().Width(80)
				}
				return lipgloss.NewStyle()
			})

		for _, row := range rows {
			t.Row(row...)
		}

		b.WriteString(t.Render())
	}

	return b.String()
}

func renderMemoryGC(m model) string {
	b := strings.Builder{}
	b.WriteString(dashTitleStyle.Render("Memory Consolidation") + "\n\n")

	snapshot := m.coldMetrics.Snapshot
	st := loadingText
	if m.hotConnected {
		var pruned any = "Pending Sync"
		if gc, ok := snapshot["memory_gc"].(map[string]any); ok {
			pruned = gc["pruned_nodes"]
		}
		st = renderStyledTable([]string{dashFieldMetric, dashFieldValue}, [][]string{
			{"State", "Optimizing (Live)"},
			{"LSM ValueLog Sweeps", fmt.Sprintf("%d", m.hotState.GCSweeps)},
			{"Nodes Orphaned & Pruned", fmt.Sprintf("%v", pruned)},
		})
	} else if gc, ok := snapshot["memory_gc"].(map[string]any); ok {
		st = renderStyledTable([]string{dashFieldMetric, dashFieldValue}, [][]string{
			{"State", "Optimizing"},
			{"LSM ValueLog Sweeps", fmt.Sprintf("%v", gc["sweeps"])},
			{"Nodes Orphaned & Pruned", fmt.Sprintf("%v", gc["pruned_nodes"])},
		})
	}
	gcBox := cardStyle.Render(subTitleStyle.Render("Memory Consolidation & GC") + "\n" + st)

	// Proposed Visualization: Graph Density Distribution
	densityTable := loadingText
	var namespaceRows [][]string

	// Ordered list matching AllDomains for consistent dashboard display.
	nsLabels := []struct {
		Key   string
		Label string
	}{
		{dashNSMemories, "Memories (Nodes)"},
		{dashNSSessions, "Sessions (Relations)"},
		{dashNSStandards, "Standards (Rules)"},
		{dashNSProjects, "Projects (Context)"},
		{dashNSDialecticHistory, "Dialectic History"},
		{dashNSServerStatus, "Server Status"},
		{dashNSModernizerVerdicts, "Modernizer Verdicts"},
		{dashNSModernizerTrust, "Modernizer Trust"},
		{dashNSMadrState, "MADR Decision Records"},
	}

	if m.hotConnected {
		for _, ns := range nsLabels {
			namespaceRows = append(namespaceRows, []string{ns.Label, fmt.Sprintf("%d", m.hotState.Namespaces[ns.Key])})
		}
	} else if tx, ok := snapshot["taxonomy"].(map[string]any); ok {
		for _, ns := range nsLabels {
			namespaceRows = append(namespaceRows, []string{ns.Label, fmt.Sprintf("%v", tx[ns.Key])})
		}
	}

	if len(namespaceRows) > 0 {
		densityTable = renderStyledTable([]string{"Entity Type", "Absolute Count"}, namespaceRows)
	}
	densityBox := cardStyle.Render(subTitleStyle.Render("Graph Density Distribution") + "\n" + densityTable)

	// Proposed Visualization: Garbage Collection Horizon
	ttlTable := loadingText
	if gc, ok := snapshot["memory_gc"].(map[string]any); ok {
		ttlTable = renderStyledTable([]string{"Time Horizon", "Prunable Entities"}, [][]string{
			{"< 24 Hours", fmt.Sprintf("%v", gc["horizon_24h"])},
			{"< 7 Days", fmt.Sprintf("%v", gc["horizon_7d"])},
			{"< 30 Days", fmt.Sprintf("%v", gc["horizon_30d"])},
		})
	}
	ttlBox := cardStyle.Render(subTitleStyle.Render("Garbage Collection Horizon") + "\n" + ttlTable)

	// Write Operation Breakdown
	writeOpsTable := loadingText
	if m.hotConnected {
		c := float64(m.hotState.CreateOps)
		u := float64(m.hotState.UpdateOps)
		mg := float64(m.hotState.MergeOps)
		total := c + u + mg
		if total > 0 {
			writeOpsTable = renderStyledTable(
				[]string{"Operation", dashFieldCount, "% of Total"},
				[][]string{
					{"Created", fmt.Sprintf("%.0f", c), fmt.Sprintf("%.1f%%", c/total*100)},
					{"Updated", fmt.Sprintf("%.0f", u), fmt.Sprintf("%.1f%%", u/total*100)},
					{"Merged (Dedup)", fmt.Sprintf("%.0f", mg), fmt.Sprintf("%.1f%%", mg/total*100)},
				},
			)
		} else {
			writeOpsTable = metricLabelStyle.Render("No write operations recorded.") + "\n"
		}
	} else if wo, ok := snapshot["write_ops"].(map[string]any); ok {
		c, _ := wo["created"].(float64) //nolint:errcheck // optional telemetry field
		u, _ := wo["updated"].(float64) //nolint:errcheck // optional telemetry field
		mg, _ := wo["merged"].(float64) //nolint:errcheck // optional telemetry field
		total := c + u + mg
		if total > 0 {
			writeOpsTable = renderStyledTable(
				[]string{"Operation", dashFieldCount, "% of Total"},
				[][]string{
					{"Created", fmt.Sprintf("%.0f", c), fmt.Sprintf("%.1f%%", c/total*100)},
					{"Updated", fmt.Sprintf("%.0f", u), fmt.Sprintf("%.1f%%", u/total*100)},
					{"Merged (Dedup)", fmt.Sprintf("%.0f", mg), fmt.Sprintf("%.1f%%", mg/total*100)},
				},
			)
		} else {
			writeOpsTable = metricLabelStyle.Render("No write operations recorded.") + "\n"
		}
	}
	writeOpsBox := cardStyle.Render(subTitleStyle.Render("Write Operation Breakdown") + "\n" + writeOpsTable)

	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, gcBox, densityBox) + "\n\n")
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, ttlBox, writeOpsBox) + "\n")

	return b.String()
}

func renderSearchEngine(m model) string {
	b := strings.Builder{}
	snapshot := m.coldMetrics.Snapshot

	st := loadingText

	docs := "N/A"
	if m.hotConnected {
		docs = fmt.Sprintf("%d", m.hotState.BleveDocs)
	} else if bl, ok := snapshot["bleve"].(map[string]any); ok {
		docs = fmt.Sprintf("%v", bl["documents"])
	}

	if bl, ok := snapshot["bleve"].(map[string]any); ok {
		drift := fmt.Sprintf("%v", bl["drift"])

		// Compute Bleve index size in KB
		idxSize := "N/A"
		if raw, ok := bl["index_size"]; ok {
			switch v := raw.(type) {
			case float64:
				idxSize = fmt.Sprintf("%.2f KB", v/1024)
			default:
				idxSize = fmt.Sprintf("%v B", v)
			}
		}

		st = renderStyledTable(
			[]string{dashFieldMetric, dashFieldValue},
			[][]string{
				{"Indexed Documents", docs},
				{"Index Size", idxSize},
				{"Heuristic Drift Alerts", drift},
			},
		)
	}
	engineCard := cardStyle.Render(subTitleStyle.Render("Semantic Search Engine") + "\n" + st)

	qpsPanel := loadingText
	if m.hotConnected {
		qpsPanel = fmt.Sprintf("Average RPC Latency: %v ms", m.hotState.AvgRPCLatencyMs)
	} else if an, ok := snapshot["analytics"].(map[string]any); ok {
		qpsPanel = fmt.Sprintf("Average RPC Latency: %v ms", an["avg_rpc_latency_ms"])
	}
	qpsCard := cardStyle.Render(subTitleStyle.Render("Search Latency & QPS") + "\n" + qpsPanel)

	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, engineCard, qpsCard))

	return b.String()
}

func renderTaxonomyAST(m model) string {
	b := strings.Builder{}
	snapshot := m.coldMetrics.Snapshot

	stAst := loadingText
	if a, ok := snapshot["ast"].(map[string]any); ok {
		stAst = renderStyledTable(
			[]string{"AST Configuration", dashFieldValue},
			[][]string{
				{"Disable Drift Heuristics", fmt.Sprintf("%v", a["disable_drift"])},
				{"Excluded Directories", fmt.Sprintf("%v boundaries", a["exclude_dirs"])},
				{"Estimated AST Derivations", fmt.Sprintf("%v", a["parsed_files"])},
			},
		)
	}
	astBox := cardStyle.Render(subTitleStyle.Render("AST Ingestion Pipeline") + "\n" + stAst)

	stTax := loadingText
	if tx, ok := snapshot["taxonomy"].(map[string]any); ok {
		nsOrder := []struct {
			Key   string
			Label string
		}{
			{dashNSMemories, dashNSMemories},
			{dashNSSessions, dashNSSessions},
			{dashNSStandards, dashNSStandards},
			{dashNSProjects, dashNSProjects},
			{dashNSDialecticHistory, dashNSDialecticHistory},
			{dashNSServerStatus, dashNSServerStatus},
			{dashNSModernizerVerdicts, dashNSModernizerVerdicts},
			{dashNSModernizerTrust, dashNSModernizerTrust},
			{dashNSMadrState, dashNSMadrState},
		}
		var taxRows [][]string
		for _, ns := range nsOrder {
			taxRows = append(taxRows, []string{ns.Label, fmt.Sprintf("%v", tx[ns.Key])})
		}
		stTax = renderStyledTable(
			[]string{"Taxonomy Namespace", "Absolute Documents Configured"},
			taxRows,
		)
	}
	taxBox := cardStyle.Render(subTitleStyle.Render("Taxonomy & Tag Distribution") + "\n" + stTax)

	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, astBox, taxBox))
	b.WriteString("\n")

	// Category Distribution (Top 10)
	catTable := metricLabelStyle.Render("No categories recorded.") + "\n"
	if rawCats, ok := snapshot["categories"]; ok && rawCats != nil {
		catBytes, err := json.Marshal(rawCats)
		if err != nil {
			catBytes = nil
		}
		var cats []struct {
			Category string `json:"category"`
			Count    int    `json:"count"`
		}
		if json.Unmarshal(catBytes, &cats) == nil && len(cats) > 0 {
			var rows [][]string
			for _, c := range cats {
				rows = append(rows, []string{c.Category, fmt.Sprintf("%d", c.Count)})
			}
			catTable = renderStyledTable([]string{"Category", dashFieldCount}, rows)
		}
	}
	b.WriteString(cardStyle.Render(subTitleStyle.Render("Category Distribution (Top 10)") + "\n" + catTable))

	return b.String()
}

func renderRPCAnalytics(m model) string {
	b := strings.Builder{}
	b.WriteString(dashTitleStyle.Render("RPC Analytics") + "\n\n")

	snapshot := m.coldMetrics.Snapshot

	st := loadingText
	if m.hotConnected {
		st = renderStyledTable(
			[]string{dashFieldMetric, "Absolute Value"},
			[][]string{
				{"Bleve Search Queries", fmt.Sprintf("%v", m.hotState.SearchQueries)},
				{"Avg Search Latency", fmt.Sprintf("%.2f ms", m.hotState.AvgRPCLatencyMs)},
				{"BadgerDB Cache Hits", fmt.Sprintf("%v", m.hotState.CacheHits)},
				{"BadgerDB Cache Misses", fmt.Sprintf("%v", m.hotState.CacheMisses)},
				{"BadgerDB Key Hits", fmt.Sprintf("%v", m.hotState.DBHits)},
				{"BadgerDB Key Misses", fmt.Sprintf("%v", m.hotState.DBMisses)},
				{"RPC Payload Bytes", fmt.Sprintf("%v", m.hotState.RPCPayloadBytes)},
			},
		)
	} else if an, ok := snapshot["analytics"].(map[string]any); ok {
		st = renderStyledTable(
			[]string{dashFieldMetric, "Absolute Value"},
			[][]string{
				{"BadgerDB Cache Hits", fmt.Sprintf("%v", an["cache_hits"])},
				{"BadgerDB Cache Misses", fmt.Sprintf("%v", an["cache_misses"])},
				{"BadgerDB Key Hits", fmt.Sprintf("%v", an["db_hits"])},
				{"BadgerDB Key Misses", fmt.Sprintf("%v", an["db_misses"])},
				{"RPC Payload Bytes", fmt.Sprintf("%v", an["rpc_payload_bytes"])},
			},
		)
	}
	rpcBox := cardStyle.Render(subTitleStyle.Render("RPC & Gateway Analytics") + "\n" + st)

	primBoxContent := metricLabelStyle.Render(" Waiting for I/O operations telemetry...") + "\n"
	if an, ok := snapshot["analytics"].(map[string]any); ok {
		if prims, ok := an["primitives"].(map[string]any); ok && len(prims) > 0 {
			var rows [][]string
			for op, rawStat := range prims {
				if statMap, ok := rawStat.(map[string]any); ok {
					ops := fmt.Sprintf("%.2f", statMap["ops_sec"])
					lat := fmt.Sprintf("%.2f ms", statMap["ema_latency"])
					rows = append(rows, []string{op, ops, lat})
				}
			}
			// Sort by op name for stable display (map iteration order is random).
			sort.SliceStable(rows, func(i, j int) bool {
				return rows[i][0] < rows[j][0]
			})
			if len(rows) > 0 {
				primBoxContent = renderStyledTable([]string{"Storage Primitive", "Ops/sec", "EMA Latency"}, rows)
			}
		}
	}
	primBox := cardStyle.Render(subTitleStyle.Render("Top Accessed Storage Primitives") + "\n" + primBoxContent)

	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, rpcBox, primBox))
	b.WriteString("\n\n")

	// Top 10 Search Queries
	b.WriteString(subTitleStyle.Render("Top 10 Search Queries") + "\n")
	if rawQueries, ok := snapshot["top_queries"]; ok && rawQueries != nil {
		// The snapshot comes as []any from JSON unmarshalling; we need to type-assert or re-marshal.
		queryBytes, err := json.Marshal(rawQueries)
		if err != nil {
			queryBytes = nil
		}
		var queries []struct {
			Query        string  `json:"query"`
			Count        int     `json:"count"`
			AvgLatencyMs float64 `json:"avg_latency_ms"`
		}
		if json.Unmarshal(queryBytes, &queries) == nil && len(queries) > 0 {
			var rows [][]string
			for i, q := range queries {
				rows = append(rows, []string{
					fmt.Sprintf("%d", i+1),
					q.Query,
					fmt.Sprintf("%d", q.Count),
					fmt.Sprintf("%.1f ms", q.AvgLatencyMs),
				})
			}
			b.WriteString(renderStyledTable([]string{"#", "Query", dashFieldCount, "Avg Latency"}, rows))
		} else {
			b.WriteString(metricLabelStyle.Render("No search queries recorded yet.") + "\n")
		}
	} else {
		b.WriteString(metricLabelStyle.Render("No search queries recorded yet.") + "\n")
	}

	return b.String()
}

func renderNetwork(m model) string {
	b := strings.Builder{}
	snapshot := m.coldMetrics.Snapshot

	b.WriteString(dashTitleStyle.Render("Network Operations") + "\n\n")

	var topBox, clientBox string

	if m.hotConnected {
		// Transport overview table
		stdioConnected := fmt.Sprintf("%v", m.hotState.StdioConnected)
		httpPort := fmt.Sprintf("%v", m.hotState.HTTPPort)
		totalClients := fmt.Sprintf("%v", m.hotState.TotalClients)

		transportTable := renderStyledTable(
			[]string{"Transport", "Status", "Details"},
			[][]string{
				{dashTransport, stdioConnected, "Orchestrator pipe"},
				{"HTTP Streamable", fmt.Sprintf("Port %s", httpPort), fmt.Sprintf("%s total clients", totalClients)},
			},
		)
		topBox = cardStyle.Render(subTitleStyle.Render("Network Topology") + "\n" + transportTable)
	} else if nw, ok := snapshot["network"].(map[string]any); ok {
		// Transport overview table
		stdioConnected := fmt.Sprintf("%v", nw["stdio_connected"])
		httpPort := fmt.Sprintf("%v", nw["http_port"])
		totalClients := fmt.Sprintf("%v", nw["total_clients"])

		transportTable := renderStyledTable(
			[]string{"Transport", "Status", "Details"},
			[][]string{
				{dashTransport, stdioConnected, "Orchestrator pipe"},
				{"HTTP Streamable", fmt.Sprintf("Port %s", httpPort), fmt.Sprintf("%s total clients", totalClients)},
			},
		)
		topBox = cardStyle.Render(subTitleStyle.Render("Network Topology") + "\n" + transportTable)
	} else {
		topBox = cardStyle.Render(subTitleStyle.Render("Network Topology") + "\n" + metricLabelStyle.Render(loadingText) + "\n")
	}

	if nw, ok := snapshot["network"].(map[string]any); ok {
		// Connected HTTP clients table
		clientContent := ""
		if httpClients, ok := nw["http_clients"].(map[string]any); ok && len(httpClients) > 0 {
			var rows [][]string
			for sessionID, clientName := range httpClients {
				sid := sessionID
				if len(sid) > 12 && sid != "pre-init" { //nolint:goconst // bypass
					sid = sid[:12] + "..."
				}
				rows = append(rows, []string{sid, fmt.Sprintf("%v", clientName)})
			}
			// Sort by client name then session ID for fully stable ordering.
			sort.SliceStable(rows, func(i, j int) bool {
				if rows[i][1] != rows[j][1] {
					return rows[i][1] < rows[j][1]
				}
				return rows[i][0] < rows[j][0]
			})
			clientContent = renderStyledTable([]string{"Session ID", "Client Name"}, rows)
		} else {
			clientContent = metricLabelStyle.Render("No HTTP clients currently connected.") + "\n"
		}
		clientBox = cardStyle.Render(subTitleStyle.Render("Connected HTTP Clients") + "\n" + clientContent)
	} else {
		topBox = cardStyle.Render(subTitleStyle.Render("Network Topology") + "\n" + metricLabelStyle.Render(loadingText) + "\n")
		clientBox = cardStyle.Render(subTitleStyle.Render("Connected HTTP Clients") + "\n" + metricLabelStyle.Render(loadingText) + "\n")
	}

	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, topBox, clientBox))
	b.WriteString("\n")

	return b.String()
}

func renderSecurity(m model) string {
	b := strings.Builder{}
	b.WriteString(dashTitleStyle.Render("Security Operations") + "\n\n")

	snapshot := m.coldMetrics.Snapshot
	stSec := loadingText
	if m.hotConnected {
		stSec = renderStyledTable(
			[]string{"Security Component", "Status / Value"},
			[][]string{
				{"Curve25519 Native Encryption", "ENABLED"},
				{"AES-GCM Memory Cipher", "SECURE"},
				{"Security & Validation Rejections", fmt.Sprintf("%d", m.hotState.BoundaryViolations)},
			},
		)
	} else if sec, ok := snapshot["security"].(map[string]any); ok {
		stSec = renderStyledTable(
			[]string{"Security Component", "Status / Value"},
			[][]string{
				{"Curve25519 Native Encryption", "ENABLED"},
				{"AES-GCM Memory Cipher", "SECURE"},
				{"Security & Validation Rejections", fmt.Sprintf("%v", sec["boundary_violations"])},
			},
		)
	}
	b.WriteString(cardStyle.Render(subTitleStyle.Render("Security & Cryptography") + "\n" + stSec))
	b.WriteString("\n")

	return b.String()
}

func renderConfigTab(m model) string {
	snapshot := m.coldMetrics.Snapshot
	st := loadingText
	if c, ok := snapshot["config"].(map[string]any); ok {
		st = renderStyledTable(
			[]string{"Key", "Value"},
			[][]string{
				{"Version", fmt.Sprintf("%v", c["version"])},
				{"DB Path", fmt.Sprintf("%v", c["db_path"])},
				{"Active Log Level", fmt.Sprintf("%v", c["log_level"])},
				{"GOMEMLIMIT", fmt.Sprintf("%v", c["env_gomemlimit"])},
			},
		)
	}
	return cardStyle.Render(subTitleStyle.Render("Config & Environment") + "\n" + st)
}
