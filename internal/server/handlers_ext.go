// Package server provides functionality for the server subsystem.
package server

import (
	"context"

	"fmt"
	"strings"

	"github.com/maccavelli/mcp-server-recall/internal/memory"
	"github.com/maccavelli/mcplib"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// handleIngestFiles proxies the user path down to the concurrent memory dispatcher.
func (rs *MCPRecallServer) handleIngestFiles(ctx context.Context, req *mcp.CallToolRequest, args IngestFilesInput) (*mcp.CallToolResult, any, error) {
	if args.Path == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: path is required"}},
			IsError: true,
		}, nil, nil
	}

	targetDomain := args.Namespace
	if targetDomain == "" {
		targetDomain = memory.DomainMemories
	}

	storedCount, err := rs.store.ProcessPath(ctx, args.Path, targetDomain)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error during ingest: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	summary := fmt.Sprintf("Ingestion Complete: Processed %s, generated %d memory clips.", args.Path, storedCount)
	return &mcp.CallToolResult{
		StructuredContent: map[string]any{
			fieldSummary: summary,
			fieldData: map[string]any{
				fieldMessage: summary,
				"path":       args.Path,
				"clips":      storedCount,
			},
		},
	}, nil, nil
}

// handleDeleteMemories processes dual-mode deletions natively.
//
//nolint:unparam // MCP tool handlers must return (result, structured, error) even when structured is unused.
func (rs *MCPRecallServer) handleDeleteMemories(ctx context.Context, _ *mcp.CallToolRequest, args DeleteMemoriesInput) (*mcp.CallToolResult, any, error) {
	if !args.All && strings.TrimSpace(args.Key) == "" && strings.TrimSpace(args.Category) == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: must specify either 'key', 'category', or explicitly set 'all' to true"}},
			IsError: true,
		}, nil, nil
	}

	var summary string
	var err error

	if args.Category != "" {
		deletedCount, catErr := rs.store.DeleteByCategory(ctx, args.Category)
		if catErr != nil {
			err = catErr
		} else {
			summary = fmt.Sprintf("Deleted %d memories associated with category '%s'.", deletedCount, args.Category)
		}
	} else if args.Key != "" {
		if keyErr := rs.store.Delete(ctx, args.Key); keyErr != nil {
			err = keyErr
		} else {
			summary = fmt.Sprintf("Deleted memory '%s'.", args.Key)
		}
	} else if args.All {
		deletedCount, allErr := rs.store.DeleteDomain(ctx, memory.DomainMemories)
		if allErr != nil {
			err = allErr
		} else {
			summary = fmt.Sprintf("Deleted ALL %d memory records.", deletedCount)
		}
	}

	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error deleting memories: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	return &mcp.CallToolResult{
		StructuredContent: map[string]any{
			fieldSummary: summary,
			fieldData: map[string]string{
				fieldMessage: summary,
			},
		},
	}, nil, nil
}

// handleDeleteSessions processes session deletion locally or globally.
func (rs *MCPRecallServer) handleDeleteSessions(ctx context.Context, _ *mcp.CallToolRequest, args DeleteSessionsInput) (*mcp.CallToolResult, any, error) {
	targetDomain := args.Domain
	if targetDomain == "" {
		targetDomain = memory.DomainSessions
	}

	if args.Domain != "" && !rs.isNamespaceAuthorized(args.Domain) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: namespace '%s' is not supported by session handlers", args.Domain)}},
			IsError: true,
		}, nil, nil
	}

	if !args.All && strings.TrimSpace(args.Key) == "" && strings.TrimSpace(mcplib.StringValue(args.SessionID)) == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: must specify either 'key', 'session_id', or explicitly set 'all' to true"}},
			IsError: true,
		}, nil, nil
	}

	var summary string
	var err error

	if args.All {
		deletedCount, allErr := rs.store.DeleteDomain(ctx, targetDomain)
		if allErr != nil {
			err = allErr
		} else {
			summary = fmt.Sprintf("Deleted ALL %d session records.", deletedCount)
		}
	} else {
		// Pre-flight domain validation for single-key deletions.
		deleteKey := args.Key
		if deleteKey == "" && args.SessionID != nil && *args.SessionID != "" {
			suffix := ":" + *args.SessionID
			bestMatch, suffixErr := rs.store.FindSessionBySuffix(ctx, targetDomain, suffix)
			if suffixErr != nil {
				err = fmt.Errorf("error resolving session suffix: %w", suffixErr)
			} else if bestMatch == nil {
				err = fmt.Errorf("session ID %q not found in domain %q", *args.SessionID, targetDomain)
			} else {
				deleteKey = bestMatch.Key
			}
		}
		if deleteKey != "" {
			// Verify key belongs to the requested domain before deleting.
			// targetDomain is always set (defaults to DomainSessions at line 108).
			rec, getErr := rs.store.Get(ctx, deleteKey)
			if getErr != nil {
				err = fmt.Errorf("key %q not found: %w", deleteKey, getErr)
			} else if rec.Domain != targetDomain {
				err = fmt.Errorf("key %q belongs to domain %q, not requested domain %q", deleteKey, rec.Domain, targetDomain)
			} else {
				err = rs.store.Delete(ctx, deleteKey)
			}
			if err == nil {
				summary = fmt.Sprintf("Deleted session '%s'.", deleteKey)
			}
		}
	}

	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error deleting sessions: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	return &mcp.CallToolResult{
		StructuredContent: map[string]any{
			fieldSummary: summary,
			fieldData: map[string]string{
				fieldMessage: summary,
			},
		},
	}, nil, nil
}
