package server

import (
	"context"
	"testing"

	"github.com/maccavelli/mcp-server-recall/internal/memory"
)

func TestHandleSessions_LifeCycle(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// 1. Save Session
	saveReq := buildReq(`{"session_id":"s1", "state_data":"data"}`)
	saveArgs := SaveToRecallInput{
		Namespace: memory.DomainSessions,
		SessionID: new("s1"),
		StateData: "data",
		ProjectID: "p1",
		ServerID:  "srv1",
		Outcome:   "success",
	}
	res, _, err := srv.handleSaveToRecall(ctx, saveReq, saveArgs)
	if err != nil || res.IsError {
		t.Fatalf("SaveSessions failed: %v", err)
	}

	// 2. Get Session
	getReq := buildReq(`{"session_id":"s1"}`)
	getArgs := GetSessionsInput{SessionID: new("s1")}
	res, _, err = srv.handleGetSessions(ctx, getReq, getArgs)
	if err != nil || res.IsError {
		t.Fatalf("GetSessions failed: %v", err)
	}

	// 3. List Sessions
	listReq := buildReq(`{"project_id":"p1"}`)
	listArgs := ListSessionsInput{ProjectID: "p1"}
	res, _, err = srv.handleListSessions(ctx, listReq, listArgs)
	if err != nil || res.IsError {
		t.Fatalf("ListSessions failed: %v", err)
	}

	// 3a. List Sessions with TruncateContent
	longContent := make([]byte, 40000)
	for i := range longContent {
		longContent[i] = 'a'
	}
	saveArgsLong := SaveToRecallInput{
		Namespace: memory.DomainSessions,
		SessionID: new("long"),
		StateData: string(longContent),
		ProjectID: "p1",
	}
	_, _, _ = srv.handleSaveToRecall(ctx, buildReq(`{}`), saveArgsLong)

	listArgsTrunc := ListSessionsInput{ProjectID: "p1", TruncateContent: true}
	res, _, err = srv.handleListSessions(ctx, buildReq(`{}`), listArgsTrunc)
	if err != nil || res.IsError {
		t.Fatalf("ListSessions with truncate failed: %v", err)
	}

	// 4. Delete Sessions — use the full composed key (server_id:session:project_id:outcome:session_id)
	delReq := buildReq(`{"key":"srv1:session:p1:success:s1"}`)
	delArgs := DeleteSessionsInput{Key: "srv1:session:p1:success:s1"}
	res, _, err = srv.handleDeleteSessions(ctx, delReq, delArgs)
	if err != nil || res.IsError {
		t.Fatalf("DeleteSessions failed: %v", err)
	}
}

func TestHandleSearchSessions(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// Seed session
	srv.handleSaveToRecall(ctx, buildReq(`{}`), SaveToRecallInput{Namespace: memory.DomainSessions, SessionID: new("s2"), StateData: "searchable state", ProjectID: "p2"})

	searchReq := buildReq(`{"query":"searchable"}`)
	searchArgs := SearchSessionsInput{Query: "searchable"}
	res, _, err := srv.handleSearchSessions(ctx, searchReq, searchArgs)
	if err != nil || res.IsError {
		t.Fatalf("SearchSessions failed: %v", err)
	}
}

func TestHandleSessions_CrossDomainIsolation(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// 1. Save Operational Session
	srv.handleSaveToRecall(ctx, buildReq(`{}`), SaveToRecallInput{
		Namespace: memory.DomainSessions,
		SessionID: new("op1"),
		StateData: "operational",
		ProjectID: "p1",
		ServerID:  "srv1",
	})

	// 2. Save Dialectic History
	srv.handleSaveToRecall(ctx, buildReq(`{}`), SaveToRecallInput{
		Namespace: memory.DomainDialecticHistory,
		SessionID: new("dia1"),
		StateData: "dialectic",
		ProjectID: "p1",
		ServerID:  "srv1",
	})

	// 3. Bulk Delete Dialectic History
	delReq := buildReq(`{}`)
	delArgs := DeleteSessionsInput{
		Domain: memory.DomainDialecticHistory,
		All:    true,
	}
	res, _, err := srv.handleDeleteSessions(ctx, delReq, delArgs)
	if err != nil || res.IsError {
		t.Fatalf("DeleteSessions(All: true) failed: %v", err)
	}

	// 4. Verify Dialectic is gone
	getDiaArgs := GetSessionsInput{Domain: memory.DomainDialecticHistory, SessionID: new("dia1")}
	res, _, _ = srv.handleGetSessions(ctx, buildReq(`{}`), getDiaArgs)
	if !res.IsError {
		t.Fatalf("Dialectic history should be deleted, but it was found")
	}

	// 5. Verify Operational Session remains
	getOpArgs := GetSessionsInput{Domain: memory.DomainSessions, SessionID: new("op1")}
	res, _, err = srv.handleGetSessions(ctx, buildReq(`{}`), getOpArgs)
	if err != nil || res.IsError {
		t.Fatalf("Operational session should remain untouched, but it was not found or error occurred")
	}
}
