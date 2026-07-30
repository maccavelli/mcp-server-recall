package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maccavelli/mcp-server-recall/internal/util"
)

func TestAuditMiddleware_InitializeExtractsClient(t *testing.T) {
	// Create a downstream handler that records whether it was called.
	called := false
	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusAccepted)
	})

	am := newAuditMiddleware(downstream, nil)

	// Simulate an initialize request with clientInfo via Streamable HTTP.
	body := `{"jsonrpc":"2.0","method":"initialize","id":1,"params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"mcp-server-brainstorm","version":"1.0.0"}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Session-Id", "test-session-1")
	rr := httptest.NewRecorder()

	am.ServeHTTP(rr, req)

	if !called {
		t.Error("downstream handler was not called")
	}

	// Verify session was stored.
	am.mu.RLock()
	clientName, ok := am.sessions["test-session-1"]
	am.mu.RUnlock()

	if !ok || clientName != "mcp-server-brainstorm" {
		t.Errorf("expected session mapped to 'mcp-server-brainstorm', got %q (ok=%v)", clientName, ok)
	}
}

func TestAuditMiddleware_ToolCallInjectsClient(t *testing.T) {
	// Pre-populate a session mapping.
	var capturedClient string
	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedClient = util.ClientFromContext(r.Context())
		w.WriteHeader(http.StatusAccepted)
	})

	am := newAuditMiddleware(downstream, nil)
	am.sessions["sess-42"] = "mcp-server-go-refactor"

	// Simulate a tools/call request via Streamable HTTP.
	body := `{"jsonrpc":"2.0","method":"tools/call","id":2,"params":{"name":"search_memories","arguments":{"query":"test"}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Session-Id", "sess-42")
	rr := httptest.NewRecorder()

	am.ServeHTTP(rr, req)

	if capturedClient != "mcp-server-go-refactor" {
		t.Errorf("expected client 'mcp-server-go-refactor' in context, got %q", capturedClient)
	}
}

func TestAuditMiddleware_NoSessionIdPassesThrough(t *testing.T) {
	called := false
	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	am := newAuditMiddleware(downstream, nil)

	// Request without Mcp-Session-Id should pass through unmodified.
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader([]byte(`{}`)))
	rr := httptest.NewRecorder()

	am.ServeHTTP(rr, req)

	if !called {
		t.Error("downstream handler was not called for request without session ID")
	}
}

func TestAuditMiddleware_GetRequestPassesThrough(t *testing.T) {
	called := false
	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	am := newAuditMiddleware(downstream, nil)

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Mcp-Session-Id", "abc")
	rr := httptest.NewRecorder()

	am.ServeHTTP(rr, req)

	if !called {
		t.Error("downstream handler was not called for GET request")
	}
}
