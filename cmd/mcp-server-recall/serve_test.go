package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maccavelli/mcplib"
)

func TestLocalhostMiddleware(t *testing.T) {
	handlerCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})
	mw := &localhostMiddleware{next: next}

	// Test authorized
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if !handlerCalled {
		t.Errorf("expected handler to be called for 127.0.0.1")
	}

	// Test forbidden
	handlerCalled = false
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w = httptest.NewRecorder()
	mw.ServeHTTP(w, req)

	if handlerCalled {
		t.Errorf("expected handler to NOT be called for external IP")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden, got %d", w.Code)
	}
}

func TestClientRegistry(t *testing.T) {
	cr := &ClientRegistry{clients: make(map[string]string)}

	cr.Register("session1", "clientA")
	cr.Register("session2", "clientB")

	clients := cr.GetClients()
	if len(clients) != 2 {
		t.Errorf("expected 2 clients, got %d", len(clients))
	}
	if clients["session1"] != "clientA" {
		t.Errorf("expected clientA, got %s", clients["session1"])
	}

	cr.Unregister("session1")
	clients = cr.GetClients()
	if len(clients) != 1 {
		t.Errorf("expected 1 client after unregister, got %d", len(clients))
	}
	if _, ok := clients["session1"]; ok {
		t.Error("expected session1 to be removed")
	}
}

func TestNetworkInfoFunc(t *testing.T) {
	httpClients = &ClientRegistry{clients: make(map[string]string)}
	httpClients.Register("sessionX", "test-client")

	f := networkInfoFunc(8080)
	stats := f()

	if stats.HTTPPort != 8080 {
		t.Errorf("expected port 8080, got %d", stats.HTTPPort)
	}
	if !stats.StdioConnected {
		t.Error("expected StdioConnected to be true")
	}
	if stats.TotalClients != 2 { // 1 http + 1 stdio
		t.Errorf("expected 2 total clients, got %d", stats.TotalClients)
	}
	if stats.HTTPClients["sessionX"] != "test-client" {
		t.Errorf("expected test-client, got %s", stats.HTTPClients["sessionX"])
	}
}

func TestRunServe_CanceledContext(t *testing.T) {
	t.Setenv("MCP_RECALL_DBPATH", t.TempDir())
	t.Setenv("MCP_RECALL_SEARCHENABLED", "false")
	t.Setenv("MCP_ENDPOINT_API_PORT", "0") // Trigger HTTP API start branch

	initConfig() // This will pick up the env vars and initialize Cfg

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	// use nil or dummy readers/writers
	reader := io.NopCloser(bytes.NewReader(nil))
	writer := &nopWriteCloser{}

	logs := mcplib.NewLogBuffer()
	err := runServe(ctx, logs, reader, writer)
	if err != nil {
		// we just want it to not panic and return cleanly or with expected shutdown err
		t.Logf("runServe returned: %v", err)
	}
}

type nopWriteCloser struct{}

func (n *nopWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (n *nopWriteCloser) Close() error                { return nil }
