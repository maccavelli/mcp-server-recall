package main

import (
	"strings"
	"testing"
)

func TestDashboardViews(t *testing.T) {
	snapshot := map[string]any{
		"runtime": map[string]any{
			"memory_mb":  100,
			"goroutines": 10,
			"uptime_sec": 60,
			"num_gc":     2,
			"cpu_usage":  5.5,
		},
		"storage": map[string]any{
			"lsm_bytes":  1000,
			"vlog_bytes": 2000,
		},
		"memory_gc": map[string]any{
			"sweeps":       1,
			"pruned_nodes": 2,
		},
		"bleve": map[string]any{
			"documents": 10,
			"queues":    0,
			"drift":     0,
		},
		"analytics": map[string]any{
			"avg_rpc_latency_ms": 1.2,
			"cache_hits":         float64(10),
			"cache_misses":       float64(2),
			"db_hits":            5,
			"db_misses":          1,
			"rpc_payload_bytes":  100,
		},
		"write_ops": map[string]any{
			"created": float64(50),
			"updated": float64(20),
			"merged":  float64(5),
		},
		"batch_health": map[string]any{
			"entries_processed": float64(100),
			"errors":            float64(2),
		},
		"categories": []any{
			map[string]any{"category": "go-patterns", "count": 25},
			map[string]any{"category": "security", "count": 10},
		},
		"ast": map[string]any{
			"disable_drift": false,
			"exclude_dirs":  0,
			"parsed_files":  100,
		},
		"taxonomy": map[string]any{
			"memories":  10,
			"sessions":  5,
			"standards": 2,
			"projects":  1,
		},
		"network": map[string]any{
			"active_sessions": 2,
			"transport":       "stdio",
		},
		"security": map[string]any{
			"boundary_violations": 0,
			"auth_failures":       0,
		},
		"config": map[string]any{
			"version":        "1.0",
			"db_path":        "/tmp",
			"log_level":      "info",
			"env_gomemlimit": "",
		},
	}

	logs := []TelemetryLog{
		{Time: "2026-05-09T10:00:00Z", Level: "INFO", Msg: "msg", Pkg: "pkg"},
		{Time: "2026-05-09T10:00:00Z", Level: "DEBUG", Msg: "msg", Tool: "tool"},
		{Time: "2026-05-09T10:00:00Z", Level: "WARN", Msg: "msg", Pkg: "pkg"},
		{Time: "2026-05-09T10:00:00Z", Level: "ERROR", Msg: "msg", Pkg: "pkg"},
	}

	m := model{
		coldMetrics: coldMetricsMsg{
			Snapshot: snapshot,
			Logs:     logs,
		},
	}

	// Test every tab renders without panicking
	for i := tabOverview; i <= tabQuit; i++ {
		m.activeTab = i
		res := m.View()
		if res == "" {
			t.Errorf("Expected non-empty string for tab %d", i)
		}
	}

	m.hotConnected = true
	// Test every tab again when hot connected
	for i := tabOverview; i <= tabQuit; i++ {
		m.activeTab = i
		res := m.View()
		if res == "" {
			t.Errorf("Expected non-empty string for tab %d when hot connected", i)
		}
	}

	// Verify new data point cards appear in rendered output.
	m.activeTab = tabOverview
	overview := m.View()
	if !strings.Contains(overview, "Recall Server Overview") {
		t.Error("Expected 'Recall Server Overview' in summary tab")
	}
	if !strings.Contains(overview, "Search Hit Rate") {
		t.Error("Expected 'Search Hit Rate' in summary tab")
	}

	m.activeTab = tabMemoryGC
	gcView := m.View()
	if !strings.Contains(gcView, "Write Operation Breakdown") {
		t.Error("Expected 'Write Operation Breakdown' in memory GC tab")
	}

	m.activeTab = tabRPCAnalytics
	rpcView := m.View()
	if !strings.Contains(rpcView, "BadgerDB Cache Hits") {
		t.Error("Expected 'BadgerDB Cache Hits' in RPC analytics tab")
	}

	m.activeTab = tabTaxonomyAST
	taxView := m.View()
	if !strings.Contains(taxView, "Category Distribution") {
		t.Error("Expected 'Category Distribution' in taxonomy tab")
	}
}

func TestDashboardViewsEmptySnapshot(t *testing.T) {
	m := model{
		coldMetrics: coldMetricsMsg{
			Snapshot: make(map[string]any),
			Logs:     nil,
		},
	}

	// Test all tabs render safely with empty data
	for i := tabOverview; i <= tabQuit; i++ {
		m.activeTab = i
		res := m.View()
		if res == "" {
			t.Errorf("Expected non-empty string for empty tab %d", i)
		}
	}
}

func TestDashboardViewsPopulatedSnapshot(t *testing.T) {
	m := model{
		activeTab: tabRPCAnalytics,
		coldMetrics: coldMetricsMsg{
			Snapshot: map[string]any{
				"analytics": map[string]any{
					"cache_hits":        10,
					"cache_misses":      5,
					"db_hits":           12,
					"db_misses":         2,
					"rpc_payload_bytes": 1024,
					"primitives": map[string]any{
						"Get": map[string]any{
							"ops_sec":     150.5,
							"ema_latency": 1.2,
						},
						"Set": map[string]any{
							"ops_sec":     50.1,
							"ema_latency": 4.5,
						},
					},
				},
				"top_queries": []map[string]any{
					{"query": "test query", "count": 5, "avg_latency_ms": 1.5},
				},
				"network": map[string]any{
					"stdio_connected": true,
					"http_port":       8080,
					"total_clients":   2,
					"http_clients": map[string]any{
						"sess-123456789012345": "clientA",
						"pre-init":             "clientB",
					},
				},
			},
		},
	}

	rpcView := m.View()
	if !strings.Contains(rpcView, "Top Accessed Storage Primitives") {
		t.Error("Expected 'Top Accessed Storage Primitives' in populated RPC analytics tab")
	}

	m.activeTab = tabNetwork
	netView := m.View()
	if !strings.Contains(netView, "clientA") {
		t.Error("Expected 'clientA' in populated network tab")
	}
}

func TestDashboardModelInitAndUpdate(t *testing.T) {
	m := model{}
	cmd := m.Init()
	if cmd != nil {
		t.Errorf("Expected Init to return nil, got %v", cmd)
	}

	// Test coldMetricsMsg update
	m2, _ := m.Update(coldMetricsMsg{
		Snapshot: map[string]any{"test": true},
		Logs:     []TelemetryLog{{Msg: "test"}},
	})
	if m2.(model).coldMetrics.Snapshot["test"] != true {
		t.Error("Expected metrics to be updated")
	}

	// Test nil update
	m3, cmd := m.Update(nil)
	if cmd != nil {
		t.Errorf("Expected nil cmd for nil update")
	}
	_ = m3
}

func TestRenderStyledTable(t *testing.T) {
	result := renderStyledTable(
		[]string{"Name", "Value"},
		[][]string{
			{"CPU", "4 Cores"},
			{"Memory", "8 GB"},
		},
	)
	if result == "" {
		t.Error("Expected non-empty table output")
	}
}
