// Package server provides functionality for the server subsystem.
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/maccavelli/mcplib"

	mcpschema "github.com/maccavelli/mcplib/schema"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func add[In any](
	rs *MCPRecallServer,
	srv *mcp.Server,
	safeMap map[string]bool,
	name, desc string,
	handler func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, any, error),
) {
	if safeMap != nil && !safeMap[name] {
		return
	}
	var namespaces []string
	if rs != nil && rs.cfg != nil {
		namespaces = rs.cfg.AuthorizedNamespaces()
	}

	wrapped := func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		if rs != nil && rs.closed.Load() {
			return nil, nil, fmt.Errorf("server is shutting down")
		}
		if req != nil && req.Params != nil && len(req.Params.Arguments) > 0 {
			var check In
			dec := json.NewDecoder(bytes.NewReader(req.Params.Arguments))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&check); err != nil {
				return nil, nil, fmt.Errorf("invalid request arguments: %w", err)
			}
		}
		return handler(ctx, req, in)
	}
	mcplib.HardenedAddTool(srv, &mcp.Tool{Name: name, Description: desc}, wrapped, mcplib.WithSchemaDerival(mcpschema.Derive[In](namespaces...)))
}

func (rs *MCPRecallServer) registerAll(srv *mcp.Server, safeMap map[string]bool) {
	add(rs, srv, safeMap, "remember", "[DIRECTIVE: Dialogue Ingestion] Archives unstructured chat inputs into the semantic vector engine. Use exclusively to persist natural language human-agent interactions. [PROSE: Use this tool to save a conversational summary or informal note into the semantic memory database so it can be searched later.] Keywords: memorize-chat, archive-dialogue, persist-conversation-snippet", rs.handleRemember)
	add(rs, srv, safeMap, "recall", "[DIRECTIVE: Historical Interaction Fetcher] Pulls verbatim transcripts of previous AI-human turns using a specific chronological token. Strictly for dialogue reconstruction. [PROSE: Use this tool to retrieve exactly what was said in a past conversation by referencing its specific dialogue identifier.] Keywords: read-transcript, fetch-chat-turn, reconstruct-historical-interaction", rs.handleRecall)
	add(rs, srv, safeMap, "get_metrics", "[DIRECTIVE: Recall System Telemetry Monitor] Emits live instrumentation data for the recall server, including memory allocation, cache hit ratios, and storage volume statistics. [PROSE: Use this tool to check the system health, memory usage, or performance statistics of the recall database server.] Keywords: ram-instrumentation, runtime-statistics, monitor-storage-capacity", rs.handleGetMetrics)
	add(rs, srv, safeMap, "save_to_recall", "[DIRECTIVE: JSON Blob Writer] Writes strictly formatted JSON configuration payloads directly to a namespace partition. Used purely for structured state serialization. [PROSE: Use this tool when you have a fully formed, new standard or project specification payload that you need to save into the database.] Keywords: serialize-json-state, write-config-payload, format-structural-blob", rs.handleSaveToRecall)
	add(rs, srv, safeMap, "forget", "[DIRECTIVE: Chat Transcript Eraser] Permanently drops informal conversational dialogue entries from the matrix. Do not use for structured keys or files. [PROSE: Use this tool to erase a specific, informal conversation or dialogue snippet from memory so it is no longer referenced.] Keywords: drop-chat-snippet, erase-informal-dialogue, scrub-transcript-memory", rs.handleForget)
	add(rs, srv, safeMap, "reload_cache", "[DIRECTIVE: Recall Index Reconstruction] Forces a cold reboot of the recall Bleve full-text engine to repair matrix drift. Administrative utility to rebuild search structures. [PROSE: Use this tool if search results seem broken or out of sync, as it will completely rebuild the full-text search index from scratch.] Keywords: cold-reboot-bleve, rebuild-fulltext-engine, repair-matrix-drift", rs.handleReloadCache)
	mcplib.RegisterDiagnosticTool(
		srv,
		rs.logs,
		"recall persistence engine",
	)
	add(rs, srv, safeMap, "prune_records", "[DIRECTIVE: Recall TTL Garbage Collection] Identifies and purges stale items from the recall database that have exceeded their time-to-live threshold. Primarily used for automated lifecycle maintenance. [PROSE: Use this tool to force a garbage collection cycle and delete any records that have expired.] Keywords: ttl-cleanup, purge-stale-items, automated-lifecycle-maintenance", rs.handleContextVacuum)

	add(rs, srv, safeMap, "export_records", "[DIRECTIVE: Recall Outbound File Archiver] Serializes the complete recall database state to a JSONL file on the host filesystem for offsite disaster recovery. [PROSE: Use this tool to trigger a complete backup of the database to a local file for safekeeping.] Keywords: write-jsonl-backup, offsite-disaster-recovery, serialize-to-host-file", rs.handleExportMemories)
	add(rs, srv, safeMap, "import_records", "[DIRECTIVE: Recall Inbound Recovery Loader] Deserializes a JSONL file from the host filesystem to completely overwrite the current recall database mapping. [PROSE: Use this tool to restore the database from a previous JSONL backup file, completely wiping current data in the process.] Keywords: read-jsonl-backup, load-recovery-file, overwrite-active-database", rs.handleImportMemories)
	add(rs, srv, safeMap, "ingest_files", "[DIRECTIVE: Documentation Library Importer] Ingests physical text, markdown, JSON, or XML documentation files directly into the personal memories namespace. Use this exclusively to build out a personalized reference library. [PROSE: Use this tool to upload a file from your local disk directly into the database as a new memory or reference document.] Keywords: import-documentation-file, build-reference-library, ingest-markdown-text", rs.handleIngestFiles)

	if safeMap == nil || safeMap["search"] {
		add(rs, srv, safeMap, "search", "[DIRECTIVE: Fuzzy Semantic Scanner] Performs RAG comparisons against the embedding matrix to discover conceptually similar passages. Do not use for exact URI lookups. [PROSE: Use this tool to perform a fuzzy search across all records using natural language keywords to find relevant information.] Keywords: rag-comparisons, discover-conceptual-similarity, fuzzy-passage-scanner", rs.handleUniversalSearch)
	}
	if safeMap == nil || safeMap["list"] {
		add(rs, srv, safeMap, "list", "[DIRECTIVE: String Array Generator] Outputs a flat list of all exact URIs within a partition. Use strictly to see what identifiers exist before attempting targeted lookups. [PROSE: Use this tool to get a full list of all available keys or record names within a specific namespace, like 'standards' or 'projects'.] Keywords: output-flat-list, show-identifier-strings, partition-uri-dump", rs.handleUniversalList)
	}
	if safeMap == nil || safeMap["get"] {
		add(rs, srv, safeMap, "get", "[DIRECTIVE: Precision Payload Retriever] Retrieves strictly formatted JSON data by exact URI identifier, batch keys, or via programmatic attribute queries (tags, session_id, symbolname). [PROSE: Use this tool when you know the exact key or specific tags of a record and need to download its full content and metadata.] Keywords: retrieve-precision-data, read-specific-uri, attribute-query-retrieval", rs.handleUniversalGet)
	}
	if safeMap == nil || safeMap["delete"] {
		add(rs, srv, safeMap, "delete", "[DIRECTIVE: Targeted URI Destructor] Hard-deletes a single structural entity using its exact string identifier. Distinct from garbage collection or conversational erasure. [PROSE: Use this tool to permanently and precisely delete a specific memory, standard, or record from the database by its exact key.] Keywords: hard-delete-entity, obliterate-specific-uri, targeted-structural-removal", rs.handleUniversalDelete)
	}
	if safeMap == nil || safeMap["update_in_recall"] {
		add(rs, srv, safeMap, "update_in_recall", "[DIRECTIVE: In-Place Record Mutator] Edits a Recall DB record natively in-place by applying exact-match text replacements to its content, and allows renaming the key or updating metadata. [PROSE: Use this tool to surgically update, rename, or re-categorize an existing record in the database without downloading its full payload.] Keywords: edit, update, in-place, mutate, rename, metadata", rs.handleUpdateInRecall)
	}
}

// registerTools registers all MCP tools on the primary server.
func (rs *MCPRecallServer) registerTools() {
	rs.registerAll(rs.mcpServer, nil)
	slog.Info("All hardened and featured tools registered")
}

// ReloadTools dynamically rebuilds tool schemas and re-registers them when configuration changes.
func (rs *MCPRecallServer) ReloadTools() {
	slog.Info("Reloading MCP tools to reflect dynamic namespace configuration")

	tools := []string{
		"remember", "recall", "get_metrics", "save_to_recall", "forget",
		"reload_cache", "prune_records", "export_records", "import_records",
		"ingest_files", "search", "list", "get", "delete", "update_in_recall",
	}
	rs.mcpServer.RemoveTools(tools...)
	rs.registerTools()
}

// RegisterSafeTools assigns the safe, non-destructive queries and temporal scaffolding buckets to the SSE instance.
func (rs *MCPRecallServer) RegisterSafeTools(srv *mcp.Server) {
	safeConfig := rs.cfg.SafeTools()
	safeMap := make(map[string]bool, len(safeConfig))
	for _, name := range safeConfig {
		safeMap[name] = true
	}
	rs.registerAll(srv, safeMap)
	slog.Info("All safe tools are now registered to secondary server endpoint", fieldCount, len(safeConfig))
}

// RegisterSafeToolsInternal assigns the full administrative tool suite to the internal CLI SSE instance.
func (rs *MCPRecallServer) RegisterSafeToolsInternal(srv *mcp.Server) {
	safeConfig := rs.cfg.SafeToolsInternal()
	safeMap := make(map[string]bool, len(safeConfig))
	for _, name := range safeConfig {
		safeMap[name] = true
	}
	rs.registerAll(srv, safeMap)
	slog.Info("All internal safe tools are now registered to local server endpoint", fieldCount, len(safeConfig))
}
