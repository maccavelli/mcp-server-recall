package server

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/maccavelli/mcp-server-recall/internal/memory"

	"github.com/maccavelli/mcp-server-recall/internal/config"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func buildReq(argJSON string) *mcp.CallToolRequest {
	return &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name:      "test",
			Arguments: json.RawMessage(argJSON),
		},
	}
}

func TestHandleGetStandard_NotFound(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "standards-test-*")
	defer os.RemoveAll(tmpDir)

	store, _ := memory.NewMemoryStore(context.Background(), tmpDir, "", 0, config.New("test").BatchSettings())
	defer store.Close()

	rs := &MCPRecallServer{store: store}
	req := buildReq(`{"key": "non-existent-key"}`)

	res, _, err := rs.handleGetStandard(context.Background(), req, GetStandardInput{Key: "non-existent-key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !res.IsError {
		t.Errorf("expected IsError=true for non-existent key")
	}
}

func TestHandleDeleteStandards_NoArgs(t *testing.T) {
	rs := &MCPRecallServer{}
	req := buildReq(`{}`)
	res, _, err := rs.handleDeleteStandards(context.Background(), req, DeleteStandardsInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !res.IsError {
		t.Errorf("expected IsError=true for empty args")
	}
}

// seedStandard saves a record to the standards domain using handleSaveToRecall.
func seedStandard(t *testing.T, srv *MCPRecallServer, key, stateData string) {
	t.Helper()
	ctx := context.Background()
	res, _, err := srv.handleSaveToRecall(ctx, buildReq(`{}`), SaveToRecallInput{
		Namespace: memory.DomainStandards,
		Key:       key,
		SessionID: new(key),
		ServerID:  "standards",
		ProjectID: "global",
		Outcome:   "published",
		StateData: stateData,
	})
	if err != nil {
		t.Fatalf("seedStandard(%s) error: %v", key, err)
	}
	if res.IsError {
		t.Fatalf("seedStandard(%s) returned IsError", key)
	}
}

func TestHandleDeleteStandards_ByKey(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// Seed a standard
	seedStandard(t, srv, "STD-GO-MCP-TEST-SINGLE-001", `{"title":"test","tags":["local-standard"],"content":"test content"}`)

	// Verify it exists
	res, _, err := srv.handleGetStandard(ctx, buildReq(`{}`), GetStandardInput{Key: "STD-GO-MCP-TEST-SINGLE-001"})
	if err != nil || res.IsError {
		t.Fatalf("Standard should exist before deletion")
	}

	// Delete by key
	res, _, err = srv.handleDeleteStandards(ctx, buildReq(`{}`), DeleteStandardsInput{Key: "STD-GO-MCP-TEST-SINGLE-001"})
	if err != nil {
		t.Fatalf("Delete by key error: %v", err)
	}
	if res.IsError {
		errText := ""
		if len(res.Content) > 0 {
			if tc, ok := res.Content[0].(*mcp.TextContent); ok {
				errText = tc.Text
			}
		}
		t.Fatalf("Delete by key returned IsError: %s", errText)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "Deleted standard") {
		t.Errorf("Expected delete confirmation, got: %s", text)
	}

	// Verify it's gone
	res, _, _ = srv.handleGetStandard(ctx, buildReq(`{}`), GetStandardInput{Key: "STD-GO-MCP-TEST-SINGLE-001"})
	if !res.IsError {
		t.Errorf("Standard should be deleted but was still found")
	}
}

func TestHandleDeleteStandards_ByKey_NotFound(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	res, _, err := srv.handleDeleteStandards(ctx, buildReq(`{}`), DeleteStandardsInput{Key: "NONEXISTENT-KEY"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected IsError=true for nonexistent key")
	}
}

func TestHandleDeleteStandards_ByKey_WrongDomain(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// Save a record to sessions domain
	srv.handleSaveToRecall(ctx, buildReq(`{}`), SaveToRecallInput{
		Namespace: memory.DomainSessions,
		SessionID: new("WRONG-DOMAIN-KEY"),
		StateData: "session data",
		ProjectID: "p1",
		ServerID:  "srv1",
		Outcome:   "success",
	})

	// Try to delete it via standards namespace — should fail with domain mismatch
	res, _, err := srv.handleDeleteStandards(ctx, buildReq(`{}`), DeleteStandardsInput{Key: "srv1:session:p1:success:WRONG-DOMAIN-KEY"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected IsError=true for wrong domain key")
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "not standards") {
		t.Errorf("Expected domain mismatch error, got: %s", text)
	}
}

func TestHandleDeleteStandards_ByKeys_Batch(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// Seed 3 standards
	seedStandard(t, srv, "STD-GO-MCP-TEST-BATCH-001", `{"title":"batch1","tags":["local-standard"],"content":"content1"}`)
	seedStandard(t, srv, "STD-GO-MCP-TEST-BATCH-002", `{"title":"batch2","tags":["local-standard"],"content":"content2"}`)
	seedStandard(t, srv, "STD-GO-MCP-TEST-BATCH-003", `{"title":"batch3","tags":["local-standard"],"content":"content3"}`)

	// Delete 2 of 3
	res, _, err := srv.handleDeleteStandards(ctx, buildReq(`{}`), DeleteStandardsInput{
		Keys: []string{"STD-GO-MCP-TEST-BATCH-001", "STD-GO-MCP-TEST-BATCH-002"},
	})
	if err != nil {
		t.Fatalf("Batch delete error: %v", err)
	}
	if res.IsError {
		t.Fatalf("Batch delete returned IsError")
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "Deleted 2 of 2") {
		t.Errorf("Expected 2/2 deleted, got: %s", text)
	}

	// Verify deleted keys are gone
	res, _, _ = srv.handleGetStandard(ctx, buildReq(`{}`), GetStandardInput{Key: "STD-GO-MCP-TEST-BATCH-001"})
	if !res.IsError {
		t.Errorf("BATCH-001 should be deleted")
	}

	// Verify remaining key still exists
	res, _, err = srv.handleGetStandard(ctx, buildReq(`{}`), GetStandardInput{Key: "STD-GO-MCP-TEST-BATCH-003"})
	if err != nil || res.IsError {
		t.Errorf("BATCH-003 should still exist")
	}
}

func TestHandleDeleteStandards_ByKeys_WithMissing(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	seedStandard(t, srv, "STD-GO-MCP-TEST-PARTIAL-001", `{"title":"partial","tags":["local-standard"],"content":"content"}`)

	// Delete 1 real + 1 missing
	res, _, err := srv.handleDeleteStandards(ctx, buildReq(`{}`), DeleteStandardsInput{
		Keys: []string{"STD-GO-MCP-TEST-PARTIAL-001", "NONEXISTENT"},
	})
	if err != nil {
		t.Fatalf("Batch delete error: %v", err)
	}
	if res.IsError {
		t.Fatalf("Batch delete returned IsError")
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "Deleted 1 of 2") {
		t.Errorf("Expected 1/2 deleted, got: %s", text)
	}
	if !strings.Contains(text, "Skipped 1") {
		t.Errorf("Expected skipped count, got: %s", text)
	}
}

func TestHandleDeleteStandards_ByTags(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// Seed standards with different tags
	seedStandard(t, srv, "STD-GO-MCP-TEST-TAGGED-001", `{"title":"tagged1","tags":["local-standard","modernizer"],"content":"content1"}`)
	seedStandard(t, srv, "STD-GO-MCP-TEST-TAGGED-002", `{"title":"tagged2","tags":["local-standard","modernizer"],"content":"content2"}`)
	seedStandard(t, srv, "STD-GO-MCP-TEST-KEEP-001", `{"title":"keep","tags":["local-standard","database"],"content":"keep this"}`)

	// Delete by tag "modernizer"
	res, _, err := srv.handleDeleteStandards(ctx, buildReq(`{}`), DeleteStandardsInput{
		Tags:         []string{"modernizer"},
		TagMatchMode: "all",
	})
	if err != nil {
		t.Fatalf("Tag delete error: %v", err)
	}
	if res.IsError {
		t.Fatalf("Tag delete returned IsError: %v", res.Content)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "Deleted 2 standards") {
		t.Errorf("Expected 2 deleted, got: %s", text)
	}

	// Verify tagged standards are gone
	res, _, _ = srv.handleGetStandard(ctx, buildReq(`{}`), GetStandardInput{Key: "STD-GO-MCP-TEST-TAGGED-001"})
	if !res.IsError {
		t.Errorf("TAGGED-001 should be deleted")
	}

	// Verify untagged standard remains
	res, _, err = srv.handleGetStandard(ctx, buildReq(`{}`), GetStandardInput{Key: "STD-GO-MCP-TEST-KEEP-001"})
	if err != nil || res.IsError {
		t.Errorf("KEEP-001 should still exist")
	}
}

func TestHandleDeleteStandards_ByTags_NoMatch(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	res, _, err := srv.handleDeleteStandards(ctx, buildReq(`{}`), DeleteStandardsInput{
		Tags: []string{"nonexistent-tag-xyz"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Errorf("no-match should not be an error")
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "No standards matched") {
		t.Errorf("Expected no-match message, got: %s", text)
	}
}
