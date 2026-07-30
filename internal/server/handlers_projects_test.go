package server

import (
	"context"
	"testing"
)

func TestHandleSearchProjects(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// Empty query should return empty results but no error
	req := buildReq(`{}`)
	res, _, err := srv.handleSearchProjects(ctx, req, SearchProjectsInput{})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if res.IsError {
		t.Errorf("did not expect error in search projects result for empty query")
	}

	// Valid query
	res, _, err = srv.handleSearchProjects(ctx, req, SearchProjectsInput{Query: "test"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if res.IsError {
		t.Errorf("did not expect error in search projects result")
	}

	// Full query with filters
	res, _, err = srv.handleSearchProjects(ctx, req, SearchProjectsInput{
		Query:        "test",
		Package:      "pkg",
		SymbolType:   "func",
		Interface:    "io.Reader",
		Receiver:     "MyStruct",
		Domain:       "testdom",
		Limit:        10,
		KeyPrefix:    "pre_",
		KeySuffix:    "_suf",
		Tags:         []string{"tag1"},
		TagMatchMode: "any",
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if res.IsError {
		t.Errorf("did not expect error in search projects result with filters")
	}
}

func TestHandleDeleteProjects(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// Empty key and not all should return error
	req := buildReq(`{}`)
	res, _, err := srv.handleDeleteProjects(ctx, req, DeleteProjectsInput{})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !res.IsError {
		t.Errorf("expected isError=true for empty key")
	}
}
