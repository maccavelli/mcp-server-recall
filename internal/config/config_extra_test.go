package config

import (
	"testing"
)

func TestAuthorizedNamespaces(t *testing.T) {
	sandboxHome(t)
	c := New("1.0")
	ns := c.AuthorizedNamespaces()
	if len(ns) == 0 {
		t.Fatal("expected namespaces")
	}
}

func TestResolveAPIPort(t *testing.T) {
	t.Setenv("MCP_ENDPOINT_API_PORT", "9090")
	port := ResolveAPIPort()
	if port != 9090 {
		t.Fatalf("expected 9090, got %d", port)
	}

	t.Setenv("MCP_ENDPOINT_API_PORT", "")
	port2 := ResolveAPIPort()
	if port2 != 47669 {
		t.Fatalf("expected fallback 47669, got %d", port2)
	}
}

func TestResolveRecallURL(t *testing.T) {
	t.Setenv("MCP_REC_URL", "http://test")
	url := ResolveRecallURL()
	if url != "http://test" {
		t.Fatalf("expected http://test, got %s", url)
	}
	t.Setenv("MCP_REC_URL", "")
}
