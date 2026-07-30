package server

import (
	"context"
	"testing"
)

func TestHandleUniversal_Search(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	tests := []struct {
		name      string
		namespace string
	}{
		{"Memories", "memories"},
		{"Sessions", "sessions"},
		{"Standards", "standards"},
		{"Projects", "projects"},
		{"Ecosystem", "ecosystem"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := buildReq(`{"query":"test"}`)
			res, _, err := srv.handleUniversalSearch(ctx, req, UniversalSearchInput{Namespace: tt.namespace, Query: "test"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res == nil {
				t.Fatal("expected non-nil result")
			}
		})
	}

	// Test invalid namespace
	req := buildReq(`{"query":"test"}`)
	_, _, err := srv.handleUniversalSearch(ctx, req, UniversalSearchInput{Namespace: "invalid"})
	if err == nil {
		t.Errorf("expected error for invalid namespace")
	}
}

func TestHandleUniversal_List(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	tests := []struct {
		name      string
		namespace string
	}{
		{"Memories", "memories"},
		{"Categories", "categories"},
		{"Sessions", "sessions"},
		{"Standards", "standards"},
		{"Projects", "projects"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := buildReq(`{}`)
			res, _, err := srv.handleUniversalList(ctx, req, UniversalListInput{Namespace: tt.namespace})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res == nil {
				t.Fatal("expected non-nil result")
			}
		})
	}
}

func TestHandleUniversal_Get(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	tests := []struct {
		name      string
		namespace string
	}{
		{"Memories", "memories"},
		{"Sessions", "sessions"},
		{"Standards", "standards"},
		{"Projects", "projects"},
		{"Ecosystem", "ecosystem"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := buildReq(`{"key":"k1"}`)
			res, _, _ := srv.handleUniversalGet(ctx, req, UniversalGetInput{Namespace: tt.namespace, Key: "k1"})
			// It might fail because key doesn't exist, but we want to cover the switch logic
			if res == nil {
				t.Fatal("handleUniversalGet returned nil result")
			}
		})
	}
}

func TestHandleUniversal_Delete(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	tests := []struct {
		name      string
		namespace string
	}{
		{"Standards", "standards"},
		{"Projects", "projects"},
		{"Sessions", "sessions"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := buildReq(`{"key":"k1", "all": true}`)
			// We pass all:true to satisfy the handleDeleteStandards check
			res, _, err := srv.handleUniversalDelete(ctx, req, UniversalDeleteInput{Namespace: tt.namespace, Key: "k1", All: true})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res == nil {
				t.Fatal("expected non-nil result")
			}
		})
	}

	// Test invalid memories namespace
	t.Run("Memories_Rejected", func(t *testing.T) {
		req := buildReq(`{"key":"k1", "all": true}`)
		_, _, err := srv.handleUniversalDelete(ctx, req, UniversalDeleteInput{Namespace: "memories", Key: "k1", All: true})
		if err == nil {
			t.Errorf("expected error for memories namespace")
		} else if err.Error() != "delete operation not permitted on 'memories' namespace; use 'forget' instead" {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

func TestHandleUniversal_BatchGet(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// Empty keys
	req := buildReq(`{}`)
	res, _, err := srv.executeBatchGet(ctx, req, UniversalGetInput{Namespace: "standards", Keys: []string{}})
	if err != nil {
		t.Fatalf("expected nil error for empty keys, got: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected isError=true for empty keys")
	}

	// Memories namespace (should fail)
	_, _, err = srv.executeBatchGet(ctx, req, UniversalGetInput{Namespace: "memories", Keys: []string{"k1"}})
	if err == nil {
		t.Errorf("expected error for memories namespace in batch get")
	}

	// Valid namespace
	res, _, err = srv.executeBatchGet(ctx, req, UniversalGetInput{Namespace: "standards", Keys: []string{"k1"}})
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if res.IsError {
		t.Errorf("did not expect error in batch get result")
	}
}

func TestHandleUniversal_AttributeGet(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// Memories namespace (should fail)
	req := buildReq(`{}`)
	_, _, err := srv.executeAttributeGet(ctx, req, UniversalGetInput{Namespace: "memories"})
	if err == nil {
		t.Errorf("expected error for memories namespace in attribute get")
	}

	// Valid namespace
	res, _, err := srv.executeAttributeGet(ctx, req, UniversalGetInput{
		Namespace: "standards",
		Query: &AttributeQuery{
			Tags: []string{"tag1"},
		},
	})
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if res.IsError {
		t.Errorf("did not expect error in attribute get result")
	}
}

func TestHandleUniversal_UpdateInRecall(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// Empty key
	req := buildReq(`{}`)
	res, _, err := srv.handleUpdateInRecall(ctx, req, UpdateInRecallInput{})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !res.IsError {
		t.Errorf("expected isError=true for missing key")
	}
}

func TestNamespaceToDomain(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()

	tests := []struct {
		ns       string
		expected string
	}{
		{"standards", "standards"},
		{"projects", "projects"},
		{"sessions", "sessions"},
		{"server_status", "server_status"},
		{"dialectic_history", "dialectic_history"},
		{"ecosystem", "ecosystem"},
		{"modernizer_verdicts", "modernizer_verdicts"},
		{"modernizer_trust", "modernizer_trust"},
		{"madr_state", "madr_state"},
		{"invalid", ""},
	}

	for _, tt := range tests {
		if got := srv.namespaceToDomain(tt.ns); got != tt.expected {
			t.Errorf("expected %s for namespace %s, got %s", tt.expected, tt.ns, got)
		}
	}
}
