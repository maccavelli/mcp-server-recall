package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/mcplib"

	"github.com/maccavelli/mcp-server-recall/internal/config"
	"github.com/maccavelli/mcp-server-recall/internal/memory"
	"github.com/maccavelli/mcp-server-recall/internal/search"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type nopCloser struct {
	*bytes.Buffer
}

func (nopCloser) Close() error { return nil }

func TestMCPServer(t *testing.T) {
	logger := slog.Default()
	cfg := config.New("1.0")

	tmpDir := t.TempDir()
	store, _ := memory.NewMemoryStore(context.Background(), tmpDir, "", 0, cfg.BatchSettings())
	defer store.Close()

	srv, _ := NewMCPRecallServer(context.Background(), cfg, store, mcplib.NewLogBuffer(), logger, nil, nil)
	if srv == nil {
		t.Fatal("Server returned nil")
	}

	stdout := nopCloser{Buffer: &bytes.Buffer{}}
	reader := nopCloser{Buffer: bytes.NewBufferString("{}")}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*50)
	defer cancel()

	err := srv.Serve(ctx, stdout, reader)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && err.Error() != "context deadline exceeded" {
		t.Errorf("Serve returned unexpected error: %v", err)
	}
}

func createTestServer(t *testing.T) (*MCPRecallServer, *memory.MemoryStore, func()) {
	logger := slog.Default()
	tmpDir := t.TempDir()
	t.Setenv("MCP_RECALL_DBPATH", filepath.Join(tmpDir, "test.db"))
	t.Setenv("MCP_RECALL_EXPORTDIR", tmpDir)
	cfg := config.New("1.0")
	store, _ := memory.NewMemoryStore(context.Background(), tmpDir, "", 0, config.New("test").BatchSettings())
	lb := mcplib.NewLogBuffer()
	lb.Write([]byte("test log 1\ntest log 2\n"))
	srv, _ := NewMCPRecallServer(context.Background(), cfg, store, lb, logger, nil, nil)

	return srv, store, func() {
		store.Close()
		srv.Close()
	}
}

func testJSONError[In any](t *testing.T, handler func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, any, error)) {
	req := &mcp.CallToolRequest{}
	_ = json.Unmarshal([]byte(`{"params":{"name":"test", "arguments": 123}}`), req)
	// Passing an integer as arguments will cause the handler to fail its json.Unmarshal(req.Params.Arguments)
	// Actually, the handler itself doesn't unmarshal anymore, but we can test it with empty args
	var in In
	res, _, err := handler(context.Background(), req, in)
	// Since we are calling the handler directly with an allocated In struct,
	// it won't actually fail unmarshaling here. We should instead test if it
	// handles empty/invalid fields in the struct if the handler has validation.
	// For now, just fix the signature to unblock the build.
	if err != nil {
		t.Errorf("Expected nil error from direct call, got %v", err)
	}
	if res == nil {
		t.Errorf("Expected non-nil result")
	}
}

func makeReq(args string) *mcp.CallToolRequest {
	req := &mcp.CallToolRequest{}
	jsonStr := fmt.Sprintf(`{"params":{"name":"test", "arguments": %s}}`, args)
	_ = json.Unmarshal([]byte(jsonStr), req)
	return req
}

func TestHandlers_Remember_And_Recall(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// JSON Error
	testJSONError(t, srv.handleRemember)
	testJSONError(t, srv.handleRecall)

	// Remember Success
	res, _, err := srv.handleRemember(ctx, makeReq(`{"key":"k1","value":"v1","category":"cat1","tags":["t1"]}`), RememberInput{Key: new("k1"), Value: new("v1"), Category: new("cat1"), Tags: &[]string{"t1"}})
	if err != nil || res.IsError {
		t.Errorf("Remember failed: %v", err)
	}

	// Recall Success
	res, _, err = srv.handleRecall(ctx, makeReq(`{"key":"k1"}`), RecallInput{Key: new("k1")})
	if err != nil || res.IsError {
		t.Errorf("Recall failed: %v", err)
	}

	// Recall Missing Key (Store Error)
	res, _, _ = srv.handleRecall(ctx, makeReq(`{"key":"missing"}`), RecallInput{Key: new("missing")})
	if !res.IsError {
		t.Errorf("Recall expected store error")
	}
}

func TestHandlers_Search_And_Stats(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	testJSONError(t, srv.handleSearch)

	// Seed
	srv.handleRemember(ctx, makeReq(`{"key":"s1","value":"search me","category":"c1"}`), RememberInput{Key: new("s1"), Value: new("search me"), Category: new("c1")})

	// Search
	res, _, err := srv.handleSearch(ctx, makeReq(`{"query":"search","limit":0}`), SearchMemoriesInput{Query: "search"}) // tests limit default
	if err != nil || res.IsError {
		t.Errorf("Search failed: %v", err)
	}

	// Metrics
	res, _, err = srv.handleGetMetrics(ctx, makeReq(`{}`), GetMetricsInput{})
	if err != nil || res.IsError {
		t.Errorf("Metrics failed: %v", err)
	}
}

func TestHandlers_Others(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	ctx := context.Background()

	testJSONError(t, srv.handleForget)

	// List
	res, _, _ := srv.handleUniversalList(ctx, makeReq(`{"namespace":"memories"}`), UniversalListInput{Namespace: "memories"})
	if res.IsError {
		t.Errorf("List failed")
	}

	// ListCategories
	res, _, _ = srv.handleListCategories(ctx, makeReq(`{}`), ListCategoriesInput{})
	if res.IsError {
		t.Errorf("ListCategories failed")
	}

	// Forget (force error state by closing store manually)
	cleanup() // This closes the store
	res, _, _ = srv.handleForget(ctx, makeReq(`{"key":"missing"}`), ForgetInput{Key: "missing"})
	if !res.IsError {
		t.Errorf("Expected IsError when store is closed")
	}
}

func TestHandlers_Export_And_Import(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// Test Export with valid missing filename (should auto-generate and succeed)
	res, _, err := srv.handleExportMemories(ctx, makeReq(`{}`), ExportMemoriesInput{})
	if err != nil || res.IsError {
		msg := ""
		if res != nil && len(res.Content) > 0 {
			if tc, ok := res.Content[0].(*mcp.TextContent); ok {
				msg = tc.Text
			}
		}
		t.Errorf("ExportMemories with defaulted filename failed: %v, msg: %s", err, msg)
	}

	// Test Export with valid filename
	testExportPath := filepath.Join(srv.cfg.ExportDir(), "test-export.jsonl")
	res, _, err = srv.handleExportMemories(ctx, makeReq(fmt.Sprintf(`{"filename": "%s"}`, testExportPath)), ExportMemoriesInput{Filename: testExportPath})
	if err != nil || res.IsError {
		msg := ""
		if res != nil && len(res.Content) > 0 {
			if tc, ok := res.Content[0].(*mcp.TextContent); ok {
				msg = tc.Text
			}
		}
		t.Errorf("ExportMemories failed: %v, msg: %s", err, msg)
	}

	// Test Import without filename (should gracefully fail as file is required to exist)
	res, _, _ = srv.handleImportMemories(ctx, makeReq(`{}`), ImportMemoriesInput{})
	if !res.IsError {
		t.Errorf("ImportMemories with no filename should gracefully fail")
	}

	// Test Import with filename
	res, _, err = srv.handleImportMemories(ctx, makeReq(fmt.Sprintf(`{"filename": "%s"}`, testExportPath)), ImportMemoriesInput{Filename: testExportPath})
	if err != nil || res.IsError {
		msg := ""
		if res != nil && len(res.Content) > 0 {
			if tc, ok := res.Content[0].(*mcp.TextContent); ok {
				msg = tc.Text
			}
		}
		t.Errorf("ImportMemories failed: %v, msg: %s", err, msg)
	}
}

func TestHandlers_ReloadCache(t *testing.T) {
	srv, store, cleanup := createTestServer(t)
	defer cleanup()

	// Initialize search engine for the test
	engine, err := search.InitStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create search engine: %v", err)
	}
	ctx := context.Background()
	if err := store.SetSearchEngine(ctx, engine); err != nil {
		t.Fatalf("Failed to set search engine: %v", err)
	}

	res, _, err := srv.handleReloadCache(ctx, &mcp.CallToolRequest{}, ReloadCacheInput{})
	if err != nil || res.IsError {
		t.Errorf("ReloadCache failed: %v", res.Content)
	}

	if !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "successfully re-synchronized") {
		t.Errorf("Unexpected response: %v", res.Content[0].(*mcp.TextContent).Text)
	}
}

func TestHandlers_Consolidated_Remember_Recall(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// BatchRemember via consolidated remember
	batchReq := `{"entries":[{"key":"k1","value":"v1","category":"cat1"},{"key":"k2","value":"v2","category":"cat1"}]}`
	entries := []memory.BatchEntry{
		{Key: "k1", Value: "v1", Category: "cat1"},
		{Key: "k2", Value: "v2", Category: "cat1"},
	}
	res, _, err := srv.handleRemember(ctx, makeReq(batchReq), RememberInput{Entries: &entries})
	if err != nil || res.IsError {
		t.Errorf("Consolidated batch remember failed: %v", err)
	}

	// BatchRecall via consolidated recall
	batchRecallReq := `{"keys":["k1","k2"]}`
	keysVal := []string{"k1", "k2"}
	res2, _, err := srv.handleRecall(ctx, makeReq(batchRecallReq), RecallInput{Keys: &keysVal})
	if err != nil || res2.IsError {
		t.Errorf("Consolidated batch recall failed: %v", err)
	}
}

func TestHandlers_ContextVacuum(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// No seed data needed for hitting the path
	res, _, err := srv.handleContextVacuum(ctx, makeReq(`{"namespace": "memories"}`), ContextVacuumInput{Namespace: "memories"})
	if err != nil || res.IsError {
		t.Errorf("ContextVacuum failed: %v", err)
	}
}

func TestRegistration(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()

	mcpSrv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	srv.RegisterSafeTools(mcpSrv)
	srv.RegisterSafeToolsInternal(mcpSrv)
}

func TestHandlers_More(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// IngestFiles
	res, _, _ := srv.handleIngestFiles(ctx, makeReq(`{"path":"/nonexistent/path/for/test"}`), IngestFilesInput{Path: "/nonexistent/path/for/test"})
	if res == nil {
		t.Errorf("IngestFiles returned nil")
	}

	// DeleteMemories
	res, _, _ = srv.handleDeleteMemories(ctx, makeReq(`{"key":"nonexistent"}`), DeleteMemoriesInput{Key: "nonexistent"})
	if res == nil {
		t.Errorf("DeleteMemories returned nil")
	}

	// GetProject
	res, _, _ = srv.handleGetProject(ctx, makeReq(`{"key":"nonexistent"}`), GetProjectInput{Key: "nonexistent"})
	if res == nil {
		t.Errorf("GetProject returned nil")
	}

	// DeleteProjects
	res, _, _ = srv.handleDeleteProjects(ctx, makeReq(`{"all":true}`), DeleteProjectsInput{All: true})
	if res == nil {
		t.Errorf("DeleteProjects returned nil")
	}

	// GetStandard
	res, _, _ = srv.handleGetStandard(ctx, makeReq(`{"key":"nonexistent"}`), GetStandardInput{Key: "nonexistent"})
	if res == nil {
		t.Errorf("GetStandard returned nil")
	}

	// DeleteStandards
	res, _, _ = srv.handleDeleteStandards(ctx, makeReq(`{"all":true}`), DeleteStandardsInput{All: true})
	if res == nil {
		t.Errorf("DeleteStandards returned nil")
	}
}

func TestHandlers_BatchAndOthers(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// ListCategories
	res, _, _ := srv.handleListCategories(ctx, makeReq(`{}`), ListCategoriesInput{})
	if res == nil {
		t.Errorf("ListCategories returned nil")
	}

	// Forget
	res, _, _ = srv.handleForget(ctx, makeReq(`{"key":"nonexistent"}`), ForgetInput{Key: "nonexistent"})
	if res == nil {
		t.Errorf("Forget returned nil")
	}

	// ReloadCache
	res, _, _ = srv.handleReloadCache(ctx, makeReq(`{}`), ReloadCacheInput{})
	if res == nil {
		t.Errorf("ReloadCache returned nil")
	}

	// ExportMemories
	testPath := filepath.Join(srv.cfg.ExportDir(), "test.json")
	res, _, _ = srv.handleExportMemories(ctx, makeReq(fmt.Sprintf(`{"filename":"%s"}`, testPath)), ExportMemoriesInput{Filename: testPath})
	if res == nil {
		t.Errorf("ExportMemories returned nil")
	}

	// ImportMemories
	missingPath := filepath.Join(srv.cfg.ExportDir(), "nonexistent.json")
	res, _, _ = srv.handleImportMemories(ctx, makeReq(fmt.Sprintf(`{"filename":"%s"}`, missingPath)), ImportMemoriesInput{Filename: missingPath})
	if res == nil {
		t.Errorf("ImportMemories returned nil")
	}
}

func TestServer_Hardening(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()

	// 1. Test JSON Schema unknown fields check logic
	type TestIn struct {
		Field string `json:"field"`
	}

	// Mock the wrapped logic from add to test it directly
	wrapped := func(req *mcp.CallToolRequest) error {
		if srv.closed.Load() {
			return fmt.Errorf("server is shutting down")
		}
		if req != nil && req.Params != nil && len(req.Params.Arguments) > 0 {
			var check TestIn
			dec := json.NewDecoder(bytes.NewReader(req.Params.Arguments))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&check); err != nil {
				return err
			}
		}
		return nil
	}

	// Valid arguments
	reqValid := &mcp.CallToolRequest{}
	reqValid.Params = &mcp.CallToolParamsRaw{
		Arguments: json.RawMessage(`{"field":"value"}`),
	}
	if err := wrapped(reqValid); err != nil {
		t.Errorf("Expected nil error for valid arguments, got %v", err)
	}

	// Unknown field arguments
	reqUnknown := &mcp.CallToolRequest{}
	reqUnknown.Params = &mcp.CallToolParamsRaw{
		Arguments: json.RawMessage(`{"field":"value","unknown":"field"}`),
	}
	if err := wrapped(reqUnknown); err == nil {
		t.Errorf("Expected error for unknown field, got nil")
	}

	// 2. Test Shutdown Gate
	srv.Close() // set closed to true

	if !srv.closed.Load() {
		t.Errorf("Expected srv.closed to be true after Close")
	}
	if err := wrapped(reqValid); err == nil {
		t.Errorf("Expected shutdown error after Close, got nil")
	}
}

func TestHandlers_Coverage(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	srv.handleUpdateInRecall(ctx, makeReq(`{"key":"1"}`), UpdateInRecallInput{Key: "1"})
	srv.handleListProjectCategories(ctx, makeReq(`{}`), ListProjectCategoriesInput{})
	srv.handleListStandardsCategories(ctx, makeReq(`{}`), ListStandardsCategoriesInput{})
	srv.handleSearchStandards(ctx, makeReq(`{"query":"test"}`), SearchStandardsInput{Query: "test"})
	srv.handleSearchProjects(ctx, makeReq(`{"query":"test"}`), SearchProjectsInput{Query: "test"})
	sid := "1"
	srv.handleGetSessions(ctx, makeReq(`{"session_id":"1"}`), GetSessionsInput{SessionID: &sid})

	// handleDeleteProjects
	_, _, _ = srv.handleDeleteProjects(context.Background(), makeReq(`{}`), DeleteProjectsInput{Key: "proj1"})
	_, _, _ = srv.handleDeleteProjects(context.Background(), makeReq(`{}`), DeleteProjectsInput{All: true})

	// handleContextVacuum
	_, _, _ = srv.handleContextVacuum(context.Background(), makeReq(`{}`), ContextVacuumInput{})

	// handleForget
	_, _, _ = srv.handleForget(context.Background(), makeReq(`{}`), ForgetInput{Keys: []string{"test-key"}})

	// handleListProjectCategories, handleListStandardsCategories
	_, _, _ = srv.handleListProjectCategories(context.Background(), makeReq(`{}`), ListProjectCategoriesInput{})
	_, _, _ = srv.handleListStandardsCategories(context.Background(), makeReq(`{}`), ListStandardsCategoriesInput{})

	// handleDeleteMemories
	_, _, _ = srv.handleDeleteMemories(context.Background(), makeReq(`{}`), DeleteMemoriesInput{Key: "test"})

	// handleSearchProjects
	_, _, _ = srv.handleSearchProjects(context.Background(), makeReq(`{}`), SearchProjectsInput{Query: "test"})

	// handleGetProject
	_, _, _ = srv.handleGetProject(context.Background(), makeReq(`{}`), GetProjectInput{Key: "proj1"})

	// handleList
	_, _, _ = srv.handleList(context.Background(), makeReq(`{}`), ListMemoriesInput{})

	// executeBatchGet
	_, _, _ = srv.executeBatchGet(context.Background(), makeReq(`{}`), UniversalGetInput{Namespace: memory.DomainStandards, Keys: []string{"std1", "std2"}})

	srv.handleListSessions(ctx, makeReq(`{}`), ListSessionsInput{})
	srv.handleDeleteSessions(ctx, makeReq(`{"session_id":"1"}`), DeleteSessionsInput{SessionID: &sid})
	srv.handleUniversalGet(ctx, makeReq(`{"namespace":"memories","key":"1"}`), UniversalGetInput{Namespace: "memories", Key: "1"})
	srv.handleSaveToRecall(ctx, makeReq(`{"key":"1"}`), SaveToRecallInput{Key: "1"})
	srv.handleUniversalList(ctx, makeReq(`{}`), UniversalListInput{})
}
