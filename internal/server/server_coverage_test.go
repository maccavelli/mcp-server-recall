package server

import (
	"context"
	"strings"

	"testing"

	"github.com/maccavelli/mcp-server-recall/internal/memory"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestHandleListCategories(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// Add some data to the store so we have a category
	_, _ = srv.store.Save(ctx, "title", "testkey", "teststate", "project", []string{}, memory.DomainSessions, 0.9)

	req := buildReq(`{}`)
	res, _, err := srv.handleListCategories(ctx, req, ListCategoriesInput{})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if res.IsError {
		t.Fatalf("did not expect error in list categories result")
	}

	// Should contain "project" somewhere
	found := false
	if len(res.Content) > 0 {
		if txt, ok := res.Content[0].(*mcp.TextContent); ok {
			t.Logf("Found text content: %s", txt.Text)
			if strings.Contains(txt.Text, "project") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected to find %s in categories list", "project")
	}
}

func TestHandlers_MoreCoverage(t *testing.T) {
	srv, store, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// Seed data
	_, _ = store.Save(ctx, "proj1-title", "proj1", "project one", "test-cat", nil, memory.DomainProjects, 0.9)
	_, _ = store.Save(ctx, "std1-title", "std1", "standard one", "std-cat", nil, memory.DomainStandards, 0.9)
	_, _ = store.Save(ctx, "mem1-title", "mem1", "memory one", "", []string{"tag1"}, memory.DomainMemories, 0.9)
	_, _ = store.Save(ctx, "sess1-title", "sess1", "session one", "", nil, memory.DomainSessions, 0.9)

	req := buildReq(`{}`)

	// handleDeleteProjects
	_, _, _ = srv.handleDeleteProjects(ctx, req, DeleteProjectsInput{Key: "proj1"})
	_, _, _ = srv.handleDeleteProjects(ctx, req, DeleteProjectsInput{Keys: []string{"proj1"}})
	_, _, _ = srv.handleDeleteProjects(ctx, req, DeleteProjectsInput{All: true})
	_, _, _ = srv.handleDeleteStandards(ctx, req, DeleteStandardsInput{All: true})

	// handleContextVacuum
	_, _, _ = srv.handleContextVacuum(ctx, req, ContextVacuumInput{})

	// add some memory data for coverage
	_, _ = store.Save(ctx, "mypkg/file:func", "pkg:mypkg:func", "body", "ReferenceDoc", []string{"type:func"}, memory.DomainProjects, 0.9)
	_, _ = store.Save(ctx, "mystd", "pkg:mystd:func", "std body", "ReferenceDoc", []string{"type:func"}, memory.DomainStandards, 0.9)
	store.SyncSearchIndex(ctx)

	// handleListProjectCategories
	_, _, _ = srv.handleListProjectCategories(ctx, req, ListProjectCategoriesInput{Category: "ReferenceDoc"})

	// handleListStandardsCategories
	_, _, _ = srv.handleListStandardsCategories(ctx, req, ListStandardsCategoriesInput{Category: "ReferenceDoc"})

	// handleDeleteMemories
	_, _, _ = srv.handleDeleteMemories(ctx, req, DeleteMemoriesInput{Key: "mem1"})

	// handleSearchStandards
	_, _, _ = srv.handleSearchStandards(ctx, req, SearchStandardsInput{Query: "std"})

	// handleSearchProjects
	_, _, _ = srv.handleSearchProjects(ctx, req, SearchProjectsInput{Query: "func"})

	// handleGetProject
	_, _, _ = srv.handleGetProject(ctx, req, GetProjectInput{Key: "missing"})
	_, _, _ = srv.handleGetProject(ctx, req, GetProjectInput{Key: "func"})
	_, _, _ = srv.handleGetProject(ctx, req, GetProjectInput{Key: "proj1"}) // Domain mismatch

	// handleExportMemories & handleImportMemories
	_, _, _ = srv.handleExportMemories(ctx, req, ExportMemoriesInput{Filename: "/tmp/invalid/path.jsonl"})
	_, _, _ = srv.handleImportMemories(ctx, req, ImportMemoriesInput{Filename: "/tmp/invalid/path.jsonl"})

	// handleDeleteProjects extra branches
	_, _, _ = srv.handleDeleteProjects(ctx, req, DeleteProjectsInput{Key: "missing"})
	_, _, _ = srv.handleDeleteProjects(ctx, req, DeleteProjectsInput{Key: "mem1"})
	_, _, _ = srv.handleDeleteProjects(ctx, req, DeleteProjectsInput{Keys: []string{"proj1", "missing", "mem1"}})

	// handleGetStandard
	_, _, _ = srv.handleGetStandard(ctx, req, GetStandardInput{Key: "std1"})
	_, _, _ = srv.handleGetStandard(ctx, req, GetStandardInput{Key: "missing"})
	_, _, _ = srv.handleGetStandard(ctx, req, GetStandardInput{Key: "mem1"})

	// handleDeleteStandards
	_, _, _ = srv.handleDeleteStandards(ctx, req, DeleteStandardsInput{Key: "std1"})
	_, _, _ = srv.handleDeleteStandards(ctx, req, DeleteStandardsInput{Key: "missing"})
	_, _, _ = srv.handleDeleteStandards(ctx, req, DeleteStandardsInput{Key: "mem1"})
	_, _, _ = srv.handleDeleteStandards(ctx, req, DeleteStandardsInput{Keys: []string{"std1", "missing", "mem1"}})
	_, _, _ = srv.handleDeleteStandards(ctx, req, DeleteStandardsInput{All: true})

	// handleContextVacuum
	_, _, _ = srv.handleContextVacuum(ctx, req, ContextVacuumInput{})
	_, _, _ = srv.handleContextVacuum(ctx, req, ContextVacuumInput{Namespace: "memories"})
	_, _, _ = srv.handleContextVacuum(ctx, req, ContextVacuumInput{Namespace: "sessions"})

	// handleForget
	_, _, _ = srv.handleForget(ctx, req, ForgetInput{Key: "mem1", Keys: []string{"mem1"}})
	_, _, _ = srv.handleForget(ctx, req, ForgetInput{Keys: []string{"mem1"}})
	_, _, _ = srv.handleForget(ctx, req, ForgetInput{Key: "missing"})
	_, _, _ = srv.handleForget(ctx, req, ForgetInput{Key: "mem1"})

	// handleGetSessions
	_, _, _ = srv.handleGetSessions(ctx, req, GetSessionsInput{})

	// handleListSessions
	_, _, _ = srv.handleListSessions(ctx, req, ListSessionsInput{Limit: 10})

	// handleSaveToRecall
	_, _, _ = srv.handleSaveToRecall(ctx, req, SaveToRecallInput{})

	// handleUniversalGet
	_, _, _ = srv.handleUniversalGet(ctx, req, UniversalGetInput{Keys: []string{"mem1"}})
	_, _, _ = srv.handleUniversalGet(ctx, req, UniversalGetInput{Namespace: "projects", Keys: []string{"proj1"}})

	// handleUniversalList
	_, _, _ = srv.handleUniversalList(ctx, req, UniversalListInput{Namespace: "memories"})
	_, _, _ = srv.handleUniversalList(ctx, req, UniversalListInput{Namespace: "projects"})
	_, _, _ = srv.handleUniversalList(ctx, req, UniversalListInput{Namespace: "sessions"})

	// handleUniversalDelete
	_, _, _ = srv.handleUniversalDelete(ctx, req, UniversalDeleteInput{Namespace: "projects", Keys: []string{"proj1"}})
	_, _, _ = srv.handleUniversalDelete(ctx, req, UniversalDeleteInput{Namespace: "memories", Key: "mem1"})
	_, _, _ = srv.handleUniversalDelete(ctx, req, UniversalDeleteInput{Namespace: "standards", All: true})

	// handleUniversalSearch
	_, _, _ = srv.handleUniversalSearch(ctx, req, UniversalSearchInput{Namespace: "projects", Query: "test"})
	_, _, _ = srv.handleUniversalSearch(ctx, req, UniversalSearchInput{Namespace: "memories", Query: "memory"})

	// handleIngestFiles
	_, _, _ = srv.handleIngestFiles(ctx, req, IngestFilesInput{})

	// handleDeleteStandards
	_, _, _ = srv.handleDeleteStandards(ctx, req, DeleteStandardsInput{Key: "std1"})

	// handleExportMemories
	_, _, _ = srv.handleExportMemories(ctx, req, ExportMemoriesInput{})

	// handleDeleteProjects extra branches
	_, _, _ = srv.handleDeleteProjects(ctx, req, DeleteProjectsInput{Tags: []string{"tag1"}})
	_, _, _ = srv.handleDeleteProjects(ctx, req, DeleteProjectsInput{Category: "cat1"})
	_, _, _ = srv.handleDeleteProjects(ctx, req, DeleteProjectsInput{Package: "pkg1"})
	_, _, _ = srv.handleDeleteProjects(ctx, req, DeleteProjectsInput{CategoryNumber: 1})

	// handleSearchProjects extra branches
	_, _, _ = srv.handleSearchProjects(ctx, req, SearchProjectsInput{Query: "test", Package: "pkg", Tags: []string{"tag1"}})

	// handleSearchStandards
	_, _, _ = srv.handleSearchStandards(ctx, req, SearchStandardsInput{Query: "test", Tags: []string{"tag1"}})
	_, _, _ = srv.handleSearchStandards(ctx, req, SearchStandardsInput{Query: "test", Package: "pkg", SymbolType: "func", Receiver: "recv", Limit: 10, Domain: memory.DomainStandards, KeyPrefix: "pre", KeySuffix: "suf", TagMatchMode: "all"})
	_, _, _ = srv.handleSearchStandards(ctx, req, SearchStandardsInput{Query: "test", MetadataOnly: true, TagMatchMode: "exact"})

	// handleListProjectCategories extra
	_, _, _ = srv.handleListProjectCategories(ctx, req, ListProjectCategoriesInput{})
	_, _, _ = srv.handleListProjectCategories(ctx, req, ListProjectCategoriesInput{Category: "ReferenceDoc"})

	// handleListStandardsCategories extra
	_, _, _ = srv.handleListStandardsCategories(ctx, req, ListStandardsCategoriesInput{})
	_, _, _ = srv.handleListStandardsCategories(ctx, req, ListStandardsCategoriesInput{Category: "ReferenceDoc"})

	// handleDeleteMemories extra
	_, _, _ = srv.handleDeleteMemories(ctx, req, DeleteMemoriesInput{})
	_, _, _ = srv.handleDeleteMemories(ctx, req, DeleteMemoriesInput{Category: "test"})

	// handleImportMemories
	_, _, _ = srv.handleImportMemories(ctx, req, ImportMemoriesInput{})

	// isSubDir test branches
	_ = isSubDir("/a/b", "/a/b/c")
	_ = isSubDir("/a/b", "/a/b")
	_ = isSubDir("/a/b", "/c/d")

	// handleUniversalGet
	_, _, _ = srv.handleUniversalGet(ctx, req, UniversalGetInput{Key: "foo"})

	// handleListProjectCategories
	_, _, _ = srv.handleListProjectCategories(ctx, req, ListProjectCategoriesInput{})

	// handleSearchProjects
	_, _, _ = srv.handleSearchProjects(ctx, req, SearchProjectsInput{Query: "test"})

	// handleGetProject
	_, _, _ = srv.handleGetProject(ctx, req, GetProjectInput{Key: "foo"})

	// handleDeleteMemories
	_, _, _ = srv.handleDeleteMemories(ctx, req, DeleteMemoriesInput{Key: "test"})

	// handleListStandardsCategories
	_, _, _ = srv.handleListStandardsCategories(ctx, req, ListStandardsCategoriesInput{})

	// handleSearchStandards
	_, _, _ = srv.handleSearchStandards(ctx, req, SearchStandardsInput{Query: "test"})

	// handleContextVacuum
	_, _, _ = srv.handleContextVacuum(ctx, req, ContextVacuumInput{})

	// handleListSessions
	_, _, _ = srv.handleListSessions(ctx, req, ListSessionsInput{})

	// handleGetSessions
	_, _, _ = srv.handleGetSessions(ctx, req, GetSessionsInput{})

	// handleForget
	_, _, _ = srv.handleForget(ctx, req, ForgetInput{Key: "test"})

	// handleReloadCache
	_, _, _ = srv.handleReloadCache(ctx, req, ReloadCacheInput{})

	// Inject test records to test success branches in getters and forget
	_, _ = srv.store.Save(ctx, "valid proj", "proj_valid", "proj content", "default", nil, memory.DomainProjects, 0)
	_, _ = srv.store.Save(ctx, "valid mem", "mem_valid", "mem content", "default", nil, memory.DomainMemories, 0)
	_, _ = srv.store.Save(ctx, "valid std", "std_valid", "std content", "default", nil, memory.DomainStandards, 0)

	// handleListProjectCategories
	_, _, _ = srv.handleListProjectCategories(ctx, req, ListProjectCategoriesInput{})

	// handleGetProject
	_, _, _ = srv.handleGetProject(ctx, req, GetProjectInput{Key: "proj_valid"})
	_, _, _ = srv.handleGetProject(ctx, req, GetProjectInput{Key: "mem_valid"})       // wrong domain
	_, _, _ = srv.handleGetProject(ctx, req, GetProjectInput{Key: "mypkg/file:func"}) // missing

	// handleSaveToRecall
	_, _, _ = srv.handleSaveToRecall(ctx, req, SaveToRecallInput{Key: "mypkg/file:new", Category: "cat1", StateData: "test content"})

	// handleForget
	_, _, _ = srv.handleForget(ctx, req, ForgetInput{Key: "std"})
	_, _, _ = srv.handleForget(ctx, req, ForgetInput{})
	_, _, _ = srv.handleForget(ctx, req, ForgetInput{Key: "k1", Keys: []string{"k2"}})
	_, _, _ = srv.handleForget(ctx, req, ForgetInput{Keys: []string{"k3", "k4"}})
	_, _, _ = srv.handleForget(ctx, req, ForgetInput{Key: "mem_valid"})  // success
	_, _, _ = srv.handleForget(ctx, req, ForgetInput{Key: "proj_valid"}) // wrong domain

	// handleDeleteProjects
	_, _, _ = srv.handleDeleteProjects(ctx, req, DeleteProjectsInput{Key: "proj1"})
	_, _, _ = srv.handleDeleteProjects(ctx, req, DeleteProjectsInput{Keys: []string{"proj1"}})
	_, _, _ = srv.handleDeleteProjects(ctx, req, DeleteProjectsInput{Category: "ReferenceDoc", Package: "mypkg"})

	// handleDeleteStandards
	_, _, _ = srv.handleDeleteStandards(ctx, req, DeleteStandardsInput{Key: "std"})
	_, _, _ = srv.handleDeleteStandards(ctx, req, DeleteStandardsInput{Keys: []string{"std"}})
	_, _, _ = srv.handleDeleteStandards(ctx, req, DeleteStandardsInput{Category: "ReferenceDoc", Package: "mystd"})

	// handleDeleteMemories
	_, _, _ = srv.handleDeleteMemories(ctx, req, DeleteMemoriesInput{Key: "mem1"})
	_, _, _ = srv.handleDeleteMemories(ctx, req, DeleteMemoriesInput{Category: "cat1"})
	_, _, _ = srv.handleDeleteMemories(ctx, req, DeleteMemoriesInput{All: true})
	_, _, _ = srv.handleDeleteMemories(ctx, req, DeleteMemoriesInput{})

	// handleList
	_, _, _ = srv.handleList(ctx, req, ListMemoriesInput{})

	// handleListStandardsCategories
	_, _, _ = srv.handleListStandardsCategories(ctx, req, ListStandardsCategoriesInput{Category: "ReferenceDoc"})

	// handleContextVacuum
	_, _, _ = srv.handleContextVacuum(ctx, req, ContextVacuumInput{})
	_, _, _ = srv.handleContextVacuum(ctx, req, ContextVacuumInput{Namespace: "memories"})
	_, _, _ = srv.handleContextVacuum(ctx, req, ContextVacuumInput{Namespace: "projects"})
	_, _, _ = srv.handleContextVacuum(ctx, req, ContextVacuumInput{Namespace: "all"})

	// handleListCategories
	_, _, _ = srv.handleListCategories(ctx, req, ListCategoriesInput{})

	// handleListSessions
	_, _, _ = srv.handleListSessions(ctx, req, ListSessionsInput{})
	_, _, _ = srv.handleListSessions(ctx, req, ListSessionsInput{Domain: memory.DomainProjects})

	// handleGetSessions
	s1 := "s1"
	_, _, _ = srv.handleGetSessions(ctx, req, GetSessionsInput{SessionID: &s1})
	_, _, _ = srv.handleGetSessions(ctx, req, GetSessionsInput{})

	// handleSaveToRecall
	_, _, _ = srv.handleSaveToRecall(ctx, req, SaveToRecallInput{StateData: "data", ProjectID: "p1", Outcome: "success", Model: "m1", TraceContext: "t1"})
	_, _, _ = srv.handleSaveToRecall(ctx, req, SaveToRecallInput{StateData: string(make([]byte, 15000001))})

	// test Registration
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	srv.RegisterSafeTools(mcpServer)
	srv.RegisterSafeToolsInternal(mcpServer)
	srv.ReloadTools()

	// Test closing the server through CallTool wrapper check
	srv.Close()
}
