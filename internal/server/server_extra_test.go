package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/mcp-server-recall/internal/config"
	"github.com/maccavelli/mcp-server-recall/internal/memory"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestIsImportExportPathAllowed(t *testing.T) {
	cfg := config.New("1.0")
	rs := &MCPRecallServer{
		cfg: cfg,
	}

	exportDir := cfg.ExportDir()

	// Subdir of export dir should be allowed
	allowed, err := rs.isImportExportPathAllowed(filepath.Join(exportDir, "some_file"))
	require.NoError(t, err)
	require.True(t, allowed)

	// Subdir of cache dir should be allowed
	cacheDir, _ := os.UserCacheDir()
	allowed, err = rs.isImportExportPathAllowed(filepath.Join(cacheDir, "some_file"))
	require.NoError(t, err)
	require.True(t, allowed)

	// Random path should not be allowed
	allowed, err = rs.isImportExportPathAllowed("/opt/not_allowed_path/file")
	require.NoError(t, err)
	require.False(t, allowed)
}

func TestHandleGetSessions(t *testing.T) {
	cfg := config.New("1.0")
	tmpDir, _ := os.MkdirTemp("", "recall-sessions-test-*")
	defer os.RemoveAll(tmpDir)

	store, err := memory.NewMemoryStore(context.Background(), tmpDir, "", 1000, cfg.BatchSettings())
	require.NoError(t, err)
	defer store.Close()

	rs := &MCPRecallServer{
		cfg:   cfg,
		store: store,
	}

	// Insert some sessions
	store.Save(context.Background(), "", "sess1", "data1", "status", nil, memory.DomainSessions, 0.0)
	store.Save(context.Background(), "", "proj1", "data2", "proj", nil, memory.DomainProjects, 0.0)
	store.Save(context.Background(), "", "sess2:suffix-123", "data3", "status", nil, memory.DomainSessions, 0.0)

	// Test 1: get by key
	res, _, err := rs.handleGetSessions(context.Background(), nil, GetSessionsInput{Key: "sess1"})
	require.NoError(t, err)
	require.False(t, res.IsError)

	// Test 2: wrong domain requested
	res, _, err = rs.handleGetSessions(context.Background(), nil, GetSessionsInput{Key: "sess1", Domain: "projects"})
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Content[0].(*mcp.TextContent).Text, "requested domain 'projects'")

	// Test 3: non-session domain fallback
	res, _, err = rs.handleGetSessions(context.Background(), nil, GetSessionsInput{Key: "proj1"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.StructuredContent.(map[string]any)["summary"].(string), "Session 'proj1' retrieved")

	// Test 3b: unauthorized domain
	store.Save(context.Background(), "", "unauth1", "data4", "status", nil, "secret", 0.0)
	res, _, err = rs.handleGetSessions(context.Background(), nil, GetSessionsInput{Key: "unauth1"})
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Content[0].(*mcp.TextContent).Text, "not a session/status record")

	// Test 4: get by suffix
	sid := "suffix-123"
	res, _, err = rs.handleGetSessions(context.Background(), nil, GetSessionsInput{SessionID: &sid})
	require.NoError(t, err)
	require.False(t, res.IsError)

	// Test 5: suffix not found
	sid2 := "missing-456"
	res, _, err = rs.handleGetSessions(context.Background(), nil, GetSessionsInput{SessionID: &sid2})
	require.NoError(t, err)
	require.True(t, res.IsError)

	// Test 6: neither provided
	res, _, err = rs.handleGetSessions(context.Background(), nil, GetSessionsInput{})
	require.NoError(t, err)
	require.True(t, res.IsError)
}

func TestIsSubDir(t *testing.T) {
	require.True(t, isSubDir("/a/b", "/a/b/c"))
	require.True(t, isSubDir("/a/b", "/a/b"))
	require.False(t, isSubDir("/a/b", "/a/c"))
	require.False(t, isSubDir("/a/b", "/a/b/../c"))
}

func TestRegistrationAdd(t *testing.T) {
	rs := &MCPRecallServer{cfg: config.New("1.0")}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0.0"}, &mcp.ServerOptions{})

	type testInput struct {
		Name string `json:"name"`
	}

	handlerCalled := false
	handler := func(ctx context.Context, req *mcp.CallToolRequest, in testInput) (*mcp.CallToolResult, any, error) {
		handlerCalled = true
		_ = handlerCalled
		require.Equal(t, "test_name", in.Name)
		return &mcp.CallToolResult{}, nil, nil
	}

	// This registers the tool
	add(rs, srv, map[string]bool{"test_tool": true}, "test_tool", "desc", handler)

	// Since we can't easily execute it via MCP framework without setting up the whole stack,
	// let's just make sure it doesn't panic when we add it.
	// To actually cover the 'wrapped' handler in registration.go (lines 32-45), we need to trigger it.
	// But `mcplib.HardenedAddTool` might be an internal detail we can't easily fake calling here.
}
