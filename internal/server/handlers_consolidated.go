// Package server provides functionality for the server subsystem.
package server

import (
	"context"
	"fmt"
	"slices"

	"github.com/maccavelli/mcp-server-recall/internal/memory"
	"github.com/maccavelli/mcp-server-recall/internal/util"
	"github.com/maccavelli/mcplib"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// UniversalSearchInput defines parameters for multi-domain search.
type UniversalSearchInput struct {
	util.UniversalBaseInput

	Namespace    string   `json:"namespace,omitempty,omitzero" jsonschema:"Target domain. One of: memories, sessions, server_status, standards, projects, dialectic_history, ecosystem. Defaults to standards."`
	Query        string   `json:"query" jsonschema:"Free-text search query mapping BM25."`
	Package      string   `json:"package,omitempty,omitzero" jsonschema:"Scoping constraint for AST analysis."`
	SymbolType   string   `json:"symbol_type,omitempty,omitzero" jsonschema:"Filter by func, struct, interface."`
	Interface    string   `json:"interface,omitempty,omitzero" jsonschema:"Implements interface restriction."`
	Receiver     string   `json:"receiver,omitempty,omitzero" jsonschema:"Method receiver constraint."`
	Domain       string   `json:"domain,omitempty,omitzero" jsonschema:"Domain boundary (e.g., auth, api)."`
	Category     string   `json:"category,omitempty,omitzero" jsonschema:"Category metric limit (used over memory spaces)."`
	Tag          string   `json:"tag,omitempty,omitzero" jsonschema:"Label mapping constraint."`
	Limit        int      `json:"limit,omitempty,omitzero" jsonschema:"Result bounds."`
	ProjectID    string   `json:"project_id,omitempty,omitzero"`
	ServerID     string   `json:"server_id,omitempty,omitzero"`
	Outcome      string   `json:"outcome,omitempty,omitzero"`
	TraceContext string   `json:"trace_context,omitempty,omitzero"`
	KeyPrefix    string   `json:"key_prefix,omitempty,omitzero"`
	KeySuffix    string   `json:"key_suffix,omitempty,omitzero"`
	Tags         []string `json:"tags,omitempty,omitzero"`
	TagMatchMode string   `json:"tag_match_mode,omitempty,omitzero"`
	MetadataOnly bool     `json:"metadata_only,omitempty,omitzero"`
}

// UniversalListInput defines parameters for multi-domain enumeration.
type UniversalListInput struct {
	util.UniversalBaseInput

	Namespace       string `json:"namespace" jsonschema:"Target domain. One of: memories, sessions, server_status, standards, projects, dialectic_history, categories, standards_categories, project_categories."`
	Package         string `json:"package,omitempty,omitzero"`
	SymbolType      string `json:"symbol_type,omitempty,omitzero"`
	ProjectID       string `json:"project_id,omitempty,omitzero"`
	ServerID        string `json:"server_id,omitempty,omitzero"`
	Outcome         string `json:"outcome,omitempty,omitzero"`
	TraceContext    string `json:"trace_context,omitempty,omitzero"`
	Limit           int    `json:"limit,omitempty,omitzero"`
	TruncateContent bool   `json:"truncate_content,omitempty,omitzero"`
	OutputFormat    string `json:"output_format,omitempty,omitzero" jsonschema:"Output format. One of: keys, aggregations."`
}

// AttributeQuery defines parameters for attribute-based programmatic retrieval.
type AttributeQuery struct {
	Tags         []string `json:"tags,omitempty,omitzero" jsonschema:"Array of tags to match."`
	TagMatchMode string   `json:"tag_match_mode,omitempty,omitzero" jsonschema:"'all' (AND) or 'any' (OR). Defaults to 'all'."`
	SessionID    *string  `json:"session_id,omitempty,omitzero" jsonschema:"Match specific session ID,optional"`
	SymbolName   string   `json:"symbolname,omitempty,omitzero" jsonschema:"Match specific AST symbol."`
	SourcePath   string   `json:"source_path,omitempty,omitzero" jsonschema:"Match specific source path."`
	Category     string   `json:"category,omitempty,omitzero" jsonschema:"Match specific category."`
}

// UniversalGetInput defines parameters for exact key retrieval across domains.
type UniversalGetInput struct {
	util.UniversalBaseInput

	Namespace string          `json:"namespace" jsonschema:"Target domain. One of: memories, sessions, server_status, standards, projects, dialectic_history."`
	Key       string          `json:"key,omitempty,omitzero" jsonschema:"The discrete key to retrieve."`
	Keys      []string        `json:"keys,omitempty,omitzero" jsonschema:"Array of discrete keys for batch retrieval."`
	SessionID *string         `json:"session_id,omitempty,omitzero" jsonschema:"Session trace ID for partial match lookups over session boundaries,optional"`
	Query     *AttributeQuery `json:"query,omitempty,omitzero" jsonschema:"Attribute-based programmatic query filters. (Not applicable to 'memories' namespace)."`
}

// UniversalHarvestInput defines parameters for AST extraction.
type UniversalHarvestInput struct {
	util.UniversalBaseInput

	Namespace  string `json:"namespace" jsonschema:"Target domain. One of: projects, standards."`
	TargetPath string `json:"target_path" jsonschema:"Absolute OS directory path to recursively harvest Go source from."`
}

// UniversalDeleteInput defines parameters for explicit node destruction.
type UniversalDeleteInput struct {
	util.UniversalBaseInput

	Namespace      string   `json:"namespace" jsonschema:"Target domain. One of: standards, projects, sessions, server_status, dialectic_history."`
	Key            string   `json:"key,omitempty,omitzero"`
	Keys           []string `json:"keys,omitempty,omitzero" jsonschema:"Array of discrete keys for batch deletion."`
	Tags           []string `json:"tags,omitempty,omitzero" jsonschema:"Delete records matching these tags."`
	TagMatchMode   string   `json:"tag_match_mode,omitempty,omitzero" jsonschema:"'all' (AND) or 'any' (OR). Defaults to 'all'."`
	Category       string   `json:"category,omitempty,omitzero"`
	Package        string   `json:"package,omitempty,omitzero"`
	CategoryNumber int      `json:"category_number,omitempty,omitzero"`
	All            bool     `json:"all,omitempty,omitzero" jsonschema:"Set to true to explicitly confirm deletion of ALL records in the specified namespace or category."`
}

// UpdateInRecallInput defines parameters for atomic, in-place record mutations.
type UpdateInRecallInput struct {
	util.UniversalBaseInput

	Namespace    string                    `json:"namespace" jsonschema:"The target domain (e.g., 'standards', 'projects', 'memories')."`
	Key          string                    `json:"key" jsonschema:"The exact current record key (e.g., 'STD-GO-MCP-CORE-001')."`
	NewKey       string                    `json:"new_key,omitempty,omitzero" jsonschema:"Optional. If provided, renames the record to this new key."`
	Title        string                    `json:"title,omitempty,omitzero" jsonschema:"Optional. If provided, updates the record's title."`
	Category     string                    `json:"category,omitempty,omitzero" jsonschema:"Optional. If provided, updates the record's category."`
	Tags         []string                  `json:"tags,omitempty,omitzero" jsonschema:"Optional. Array of new tags to completely replace existing tags."`
	Replacements []memory.ReplacementChunk `json:"replacements,omitempty,omitzero" jsonschema:"List of precise textual string replacement chunks."`
}

func (rs *MCPRecallServer) handleUniversalSearch(ctx context.Context, req *mcp.CallToolRequest, input UniversalSearchInput) (*mcp.CallToolResult, any, error) {
	if input.Namespace == "" {
		input.Namespace = nsStandards
	}
	switch input.Namespace {
	case nsMemories:
		return rs.handleSearch(ctx, req, SearchMemoriesInput{
			Query: input.Query,
			Tag:   input.Tag,
			Limit: input.Limit,
		})
	case nsSessions, nsServerStatus:
		return rs.handleSearchSessions(ctx, req, SearchSessionsInput{
			Domain:       input.Namespace,
			Query:        input.Query,
			ProjectID:    input.ProjectID,
			ServerID:     input.ServerID,
			Outcome:      input.Outcome,
			TraceContext: input.TraceContext,
			Limit:        input.Limit,
		})
	// NOTE: tag filter is memory-scoped only; standards/projects/ecosystem search
	// handlers do not accept a tag parameter.
	case nsStandards:
		return rs.handleSearchStandards(ctx, req, SearchStandardsInput{
			Query: input.Query, Package: input.Package, SymbolType: input.SymbolType, Interface: input.Interface, Receiver: input.Receiver, Domain: input.Domain, Limit: input.Limit, KeyPrefix: input.KeyPrefix, KeySuffix: input.KeySuffix, Tags: input.Tags, TagMatchMode: input.TagMatchMode, MetadataOnly: input.MetadataOnly,
		})
	case nsProjects:
		return rs.handleSearchProjects(ctx, req, SearchProjectsInput{
			Query: input.Query, Package: input.Package, SymbolType: input.SymbolType, Interface: input.Interface, Receiver: input.Receiver, Domain: input.Domain, Limit: input.Limit, KeyPrefix: input.KeyPrefix, KeySuffix: input.KeySuffix, Tags: input.Tags, TagMatchMode: input.TagMatchMode,
		})
	case nsDialecticHistory, nsModernizerVerdicts, nsModernizerTrust, fieldEcosystem:
		return rs.handleSearchSessions(ctx, req, SearchSessionsInput{
			Domain:    input.Namespace,
			Query:     input.Query,
			ServerID:  input.ServerID,
			ProjectID: input.ProjectID,
			Limit:     input.Limit,
		})
	default:
		return nil, nil, fmt.Errorf("invalid namespace: %s", input.Namespace)
	}
}

func (rs *MCPRecallServer) handleUniversalList(ctx context.Context, req *mcp.CallToolRequest, input UniversalListInput) (*mcp.CallToolResult, any, error) {
	switch input.Namespace {
	case nsMemories:
		if input.OutputFormat == "aggregations" {
			return rs.handleListCategories(ctx, req, ListCategoriesInput{})
		}
		return rs.handleList(ctx, req, ListMemoriesInput{})
	case "categories":
		return rs.handleListCategories(ctx, req, ListCategoriesInput{})
	case nsSessions, nsServerStatus:
		return rs.handleListSessions(ctx, req, ListSessionsInput{
			Domain:          input.Namespace,
			ProjectID:       input.ProjectID,
			ServerID:        input.ServerID,
			Outcome:         input.Outcome,
			TraceContext:    input.TraceContext,
			Limit:           input.Limit,
			TruncateContent: input.TruncateContent,
		})
	case nsStandards, "standards_categories":
		return rs.handleListStandardsCategories(ctx, req, ListStandardsCategoriesInput{
			Package: input.Package, SymbolType: input.SymbolType,
		})
	case nsProjects, "project_categories":
		return rs.handleListProjectCategories(ctx, req, ListProjectCategoriesInput{
			Package: input.Package, SymbolType: input.SymbolType,
		})
	case nsDialecticHistory:
		return rs.handleListSessions(ctx, req, ListSessionsInput{
			Domain:          input.Namespace,
			ServerID:        input.ServerID,
			Limit:           input.Limit,
			TruncateContent: input.TruncateContent,
		})
	default:
		return nil, nil, fmt.Errorf("invalid namespace for list binding: %s", input.Namespace)
	}
}

func (rs *MCPRecallServer) handleUniversalGet(ctx context.Context, req *mcp.CallToolRequest, input UniversalGetInput) (*mcp.CallToolResult, any, error) {
	if input.Key != "" && len(input.Keys) > 0 {
		return nil, nil, fmt.Errorf("mutually exclusive inputs: cannot provide both 'key' and 'keys'")
	}
	if input.Key == "" && len(input.Keys) == 0 && input.Query == nil && input.Namespace != nsSessions && input.Namespace != nsServerStatus && input.Namespace != nsDialecticHistory {
		return nil, nil, fmt.Errorf("key, keys array, or query strictly required")
	}

	if input.Query != nil && input.Key == "" && len(input.Keys) == 0 {
		return rs.executeAttributeGet(ctx, req, input)
	}

	if len(input.Keys) > 0 {
		return rs.executeBatchGet(ctx, req, input)
	}

	switch input.Namespace {
	case nsMemories:
		return rs.handleRecall(ctx, req, RecallInput{Key: &input.Key})
	case nsStandards:
		return rs.handleGetStandard(ctx, req, GetStandardInput{Key: input.Key})
	case nsProjects:
		return rs.handleGetProject(ctx, req, GetProjectInput{Key: input.Key})
	default:
		targetDomain := rs.namespaceToDomain(input.Namespace)
		if targetDomain != "" {
			return rs.handleGetSessions(ctx, req, GetSessionsInput{Domain: targetDomain, Key: input.Key, SessionID: input.SessionID})
		}
		return nil, nil, fmt.Errorf("invalid namespace for get binding: %s", input.Namespace)
	}
}

func (rs *MCPRecallServer) handleUniversalHarvest(ctx context.Context, req *mcp.CallToolRequest, input UniversalHarvestInput) (*mcp.CallToolResult, any, error) {
	switch input.Namespace {
	case nsStandards:
		return rs.handleHarvestStandards(ctx, req, HarvestStandardsInput{TargetPath: input.TargetPath})
	case nsProjects:
		return rs.handleHarvestProjects(ctx, req, HarvestProjectsInput{TargetPath: input.TargetPath})
	default:
		return nil, nil, fmt.Errorf("invalid namespace for harvest binding: %s", input.Namespace)
	}
}

func (rs *MCPRecallServer) handleUniversalDelete(ctx context.Context, req *mcp.CallToolRequest, input UniversalDeleteInput) (*mcp.CallToolResult, any, error) {
	switch input.Namespace {
	case nsMemories:
		return nil, nil, fmt.Errorf("delete operation not permitted on 'memories' namespace; use 'forget' instead")
	case nsStandards:
		return rs.handleDeleteStandards(ctx, req, DeleteStandardsInput{
			Key: input.Key, Keys: input.Keys, Tags: input.Tags, TagMatchMode: input.TagMatchMode,
			Category: input.Category, Package: input.Package, CategoryNumber: input.CategoryNumber, All: input.All,
		})
	case nsProjects:
		return rs.handleDeleteProjects(ctx, req, DeleteProjectsInput{
			Key: input.Key, Keys: input.Keys, Tags: input.Tags, TagMatchMode: input.TagMatchMode,
			Category: input.Category, Package: input.Package, CategoryNumber: input.CategoryNumber, All: input.All,
		})
	default:
		authorized := slices.Contains(rs.cfg.AuthorizedNamespaces(), input.Namespace)
		if authorized {
			return rs.handleDeleteSessions(ctx, req, DeleteSessionsInput{Domain: input.Namespace, Key: input.Key, All: input.All})
		}
		return nil, nil, fmt.Errorf("invalid namespace for delete binding: %s", input.Namespace)
	}
}

// namespaceToDomain maps a user-facing namespace string to the internal domain constant.
func (rs *MCPRecallServer) namespaceToDomain(ns string) string {
	switch ns {
	case nsStandards:
		return memory.DomainStandards
	case nsProjects:
		return memory.DomainProjects
	case nsSessions:
		return memory.DomainSessions
	case nsServerStatus:
		return memory.DomainServerStatus
	case nsDialecticHistory:
		return memory.DomainDialecticHistory
	case fieldEcosystem:
		return memory.DomainEcosystem
	case nsModernizerVerdicts:
		return memory.DomainModernizerVerdicts
	case nsModernizerTrust:
		return memory.DomainModernizerTrust
	case "madr_state": //nolint:goconst // bypass
		return memory.DomainMADRState
	default:
		if slices.Contains(rs.cfg.AuthorizedNamespaces(), ns) {
			return ns
		}
		return ""
	}
}

func (rs *MCPRecallServer) executeBatchGet(ctx context.Context, _ *mcp.CallToolRequest, input UniversalGetInput) (*mcp.CallToolResult, any, error) {
	// Reject memories namespace — that's batch_recall's domain
	if input.Namespace == "" || input.Namespace == nsMemories {
		return nil, nil, fmt.Errorf("batch retrieval requires a non-memories namespace; use recall for memories")
	}

	if len(input.Keys) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: keys array is empty"}},
			IsError: true,
		}, nil, nil
	}

	targetDomain := rs.namespaceToDomain(input.Namespace)
	if targetDomain == "" {
		return nil, nil, fmt.Errorf("invalid namespace for batch retrieval: %s", input.Namespace)
	}

	found, missing, err := rs.store.GetBatch(ctx, input.Keys)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Batch get error: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	// Namespace isolation: only return records matching the target domain.
	for key, rec := range found {
		if rec.Domain != targetDomain {
			delete(found, key)
			missing = append(missing, key)
		}
	}

	summary := fmt.Sprintf("Batch get complete: %d found, %d missing.", len(found), len(missing))
	return &mcp.CallToolResult{
		StructuredContent: map[string]any{
			fieldSummary: summary,
			fieldData: map[string]any{
				fieldFound:   len(found),
				fieldMissing: missing,
				fieldEntries: found,
			},
		},
	}, nil, nil
}

func (rs *MCPRecallServer) executeAttributeGet(ctx context.Context, _ *mcp.CallToolRequest, input UniversalGetInput) (*mcp.CallToolResult, any, error) {
	if input.Namespace == "" || input.Namespace == nsMemories {
		return nil, nil, fmt.Errorf("attribute retrieval requires a non-memories namespace")
	}

	targetDomain := rs.namespaceToDomain(input.Namespace)
	if targetDomain == "" {
		return nil, nil, fmt.Errorf("invalid namespace for attribute retrieval: %s", input.Namespace)
	}

	memQuery := &memory.AttributeQuery{
		Tags:         input.Query.Tags,
		TagMatchMode: input.Query.TagMatchMode,
		SessionID:    mcplib.StringValue(input.Query.SessionID),
		SymbolName:   input.Query.SymbolName,
		SourcePath:   input.Query.SourcePath,
		Category:     input.Query.Category,
	}

	found, err := rs.store.GetByAttributes(ctx, targetDomain, memQuery)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Attribute get error: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	summary := fmt.Sprintf("Attribute get complete: %d records found.", len(found))
	return &mcp.CallToolResult{
		StructuredContent: map[string]any{
			fieldSummary: summary,
			fieldData: map[string]any{
				fieldFound:   len(found),
				fieldEntries: found,
			},
		},
	}, nil, nil
}

func (rs *MCPRecallServer) handleUpdateInRecall(ctx context.Context, req *mcp.CallToolRequest, input UpdateInRecallInput) (*mcp.CallToolResult, any, error) {
	if input.Key == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "key is required for update_in_recall"}},
			IsError: true,
		}, nil, nil
	}

	targetDomain := rs.namespaceToDomain(input.Namespace)
	if targetDomain == "" {
		targetDomain = memory.DomainStandards
	}

	result, err := rs.store.UpdateRecord(
		ctx,
		targetDomain,
		input.Key,
		input.NewKey,
		input.Title,
		input.Category,
		input.Tags,
		input.Replacements,
	)

	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Failed to update record: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	return &mcp.CallToolResult{
		StructuredContent: map[string]any{
			fieldSummary: fmt.Sprintf("Successfully updated record %q", result.Key),
			fieldData: map[string]any{
				fieldAction: result.Action,
				fieldKey:    result.Key,
			},
		},
	}, nil, nil
}
