package server

import (
	"context"
	"slices"
	"testing"

	"github.com/maccavelli/mcp-server-recall/internal/harvest"
	"github.com/maccavelli/mcp-server-recall/internal/memory"
)

func TestDetectDomainTags(t *testing.T) {
	tests := []struct {
		name     string
		doc      string
		expected []string
	}{
		{
			name:     "Auth Domain",
			doc:      "This function provides security and auth validation for the APIs.",
			expected: []string{"domain:auth", "domain:api"},
		},
		{
			name:     "Database Domain",
			doc:      "Performs a sql query on the abstract database.",
			expected: []string{"domain:database"}, // "sql" creates database, "database" creates database (set dedup logic naturally handles mapping, but wait, function just appends)
		},
		{
			name:     "Empty Domain",
			doc:      "Just a generic helper function passing values.",
			expected: nil,
		},
		{
			name:     "Case Insensitive",
			doc:      "Orchestrator observability metrics",
			expected: []string{"domain:observability", "domain:metrics", "domain:orchestrator"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tags := detectDomainTags(tt.doc)
			// verify tags
			for _, exp := range tt.expected {
				found := slices.Contains(tags, exp)
				if !found {
					t.Errorf("expected tag %s but got %v", exp, tags)
				}
			}
		})
	}
}

func TestBuildSymbolEntry(t *testing.T) {
	sym := harvest.HarvestedSymbol{
		Name:         "TestFunc",
		PkgPath:      "pkg/test",
		Doc:          "Handles auth routing",
		SymbolType:   "func",
		Receiver:     "Server",
		Interfaces:   []string{"Handler"},
		Dependencies: []string{"github.com/gin-gonic/gin"},
	}

	entry, err := buildSymbolEntry(sym, "standards")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if entry.Key != "pkg:pkg/test:TestFunc" {
		t.Errorf("expected key pkg:pkg/test:TestFunc, got %s", entry.Key)
	}
	if entry.Category != "HarvestedCode" {
		t.Errorf("expected HarvestedCode category, got %s", entry.Category)
	}

	expectedTags := []string{"harvested", "source:standards", "module:pkg", "type:func", "implements:Handler", "receiver:Server", "depends_on:github.com/gin-gonic/gin", "domain:auth"}
	for _, exp := range expectedTags {
		found := slices.Contains(entry.Tags, exp)
		if !found {
			t.Errorf("expected tag %s but got %v", exp, entry.Tags)
		}
	}
}

func TestHandlers_HarvestMethods(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// 1. hasDrifted should return true for new package
	if !srv.hasDrifted(ctx, memory.DomainStandards, "test/pkg", "checksum123") {
		t.Errorf("expected true for uningested package")
	}

	// 2. ingestHarvestResult
	res := &harvest.HarvestResult{
		Checksum: "checksum123",
		Symbols: []harvest.HarvestedSymbol{
			{Name: "Func1", PkgPath: "test/pkg", SymbolType: "func", Doc: "Testing domain:auth"},
		},
		PackageDocs: map[string]string{
			"test/pkg": "This is a package doc",
		},
	}
	stored, errs := srv.ingestHarvestResult(ctx, "test/pkg", res, memory.DomainStandards)
	if stored != 3 { // 1 symbol + 1 package doc + 1 drift checksum = 3 entries stored
		t.Errorf("expected 3 stored entries, got %d", stored)
	}
	if len(errs) > 0 {
		t.Errorf("expected 0 batch errors, got %d", len(errs))
	}

	// 3. hasDrifted should return false now with same checksum
	if srv.hasDrifted(ctx, memory.DomainStandards, "test/pkg", "checksum123") {
		t.Errorf("expected false for unchanged checksum")
	}

	// 4. hasDrifted should return true for different checksum
	if !srv.hasDrifted(ctx, memory.DomainStandards, "test/pkg", "checksum456") {
		t.Errorf("expected true for changed checksum")
	}

	// 5. handleHarvestStandards (Error path: engine.Run failure on invalid package)
	req := makeReq(`{"target_path":"invalid/path"}`)
	callRes, _, _ := srv.handleHarvestStandards(ctx, req, HarvestStandardsInput{TargetPath: "invalid/path"})
	if callRes == nil || !callRes.IsError {
		t.Errorf("expected error from handleHarvestStandards for invalid path")
	}

	// 6. handleHarvestProjects (Error path: engine.Run failure on invalid package)
	callRes, _, _ = srv.handleHarvestProjects(ctx, req, HarvestProjectsInput{TargetPath: "invalid/path"})
	if callRes == nil || !callRes.IsError {
		t.Errorf("expected error from handleHarvestProjects for invalid path")
	}
}

func TestExtractModuleName(t *testing.T) {
	if got := extractModuleName("github.com/gin-gonic/gin"); got != "gin" {
		t.Errorf("expected gin, got %s", got)
	}

	if got := extractModuleName("local_module"); got != "local_module" {
		t.Errorf("expected local_module, got %s", got)
	}

	if got := extractModuleName(""); got != "" {
		t.Errorf("expected empty string, got %s", got)
	}

	if got := extractModuleName("foo/bar/"); got != "foo" {
		t.Errorf("expected foo, got %s", got)
	}
}
