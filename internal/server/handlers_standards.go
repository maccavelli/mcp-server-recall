// Package server provides functionality for the server subsystem.
package server

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	"github.com/maccavelli/mcp-server-recall/internal/memory"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// standardsTools returns the tool catalog for standards-domain retrieval.

func (rs *MCPRecallServer) handleListStandardsCategories(ctx context.Context, _ *mcp.CallToolRequest, args ListStandardsCategoriesInput) (*mcp.CallToolResult, any, error) {
	groups, err := rs.store.ListStandardsOverview(ctx, args.Category)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	// Build summary stats
	totalCats := len(groups)
	totalRecs := 0
	for _, grp := range groups {
		totalRecs += grp.TotalRecords
	}

	// Build numbered listing for category_number reference
	var catNames []string
	for c := range groups {
		catNames = append(catNames, c)
	}
	sort.Strings(catNames)

	var sb strings.Builder
	fmt.Fprintf(&sb, "Standards overview: %d %s, %d %s.\n\n", totalCats, plural(totalCats, "category", "categories"), totalRecs, plural(totalRecs, "record", "records"))
	for i, name := range catNames {
		grp := groups[name]
		fmt.Fprintf(&sb, "%d. %s (%d %s)\n", i+1, name, grp.TotalRecords, plural(grp.TotalRecords, "record", "records"))
		for _, r := range grp.Records {
			if r.Title != "" {
				fmt.Fprintf(&sb, "   - %s — %s\n", r.Key, r.Title)
			} else {
				fmt.Fprintf(&sb, "   - %s\n", r.Key)
			}
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: sb.String()}},
	}, nil, nil
}

func (rs *MCPRecallServer) handleSearchStandards(ctx context.Context, _ *mcp.CallToolRequest, args SearchStandardsInput) (*mcp.CallToolResult, any, error) {
	if args.Limit <= 0 {
		args.Limit = rs.cfg.DefaultPagination()
	}

	resultsSeq, err := rs.store.SearchStandards(ctx, memory.SearchDomainQuery{
		Query:         args.Query,
		PackageFilter: args.Package,
		SymbolType:    args.SymbolType,
		Interface:     args.Interface,
		Receiver:      args.Receiver,
		Domain:        args.Domain,
		Limit:         args.Limit,
		KeyPrefix:     args.KeyPrefix,
		KeySuffix:     args.KeySuffix,
		Tags:          args.Tags,
		TagMatchMode:  args.TagMatchMode,
	})
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	var results []*memory.SearchResult
	for r := range resultsSeq {
		results = append(results, r)
	}

	// Build filter summary for context
	filtersApplied := map[string]string{}
	if args.Package != "" {
		filtersApplied["package"] = args.Package
	}
	if args.SymbolType != "" {
		filtersApplied["symbol_type"] = args.SymbolType
	}
	if args.Interface != "" {
		filtersApplied["interface"] = args.Interface
	}
	if args.Receiver != "" {
		filtersApplied["receiver"] = args.Receiver
	}
	if args.Domain != "" {
		filtersApplied[fieldDomain] = args.Domain
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Standards search for '%s': %d results.\n", args.Query, len(results))
	if len(filtersApplied) > 0 {
		sb.WriteString("Filters: ")
		for k, v := range filtersApplied {
			fmt.Fprintf(&sb, "%s=%s ", k, v)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	for _, r := range results {
		fmt.Fprintf(&sb, "- [%s] %s\n  Key: %s\n", r.Record.Category, r.Key, r.Key)
		if r.Summary != "" {
			fmt.Fprintf(&sb, "  %s\n", r.Summary)
		}
		if len(r.Snippets) > 0 {
			for _, snip := range r.Snippets {
				fmt.Fprintf(&sb, "    ... %s ...\n", strings.TrimSpace(snip))
			}
		}
	}

	// Build structured entries for machine consumers
	entries := make([]map[string]any, 0, len(results))
	for _, r := range results {
		hashStr := fmt.Sprintf("%x", sha256.Sum256([]byte(r.Record.Content)))
		entry := map[string]any{
			fieldKey:      r.Key,
			fieldCategory: r.Record.Category,
			"score":       r.Score,
			"hash":        hashStr,
			fieldTags:     r.Record.Tags,
		}
		if !args.MetadataOnly {
			entry[fieldContent] = r.Record.Content
		}
		if r.Summary != "" {
			entry[fieldSummary] = r.Summary
		}
		if len(r.Snippets) > 0 {
			entry["snippets"] = r.Snippets
		}
		entries = append(entries, entry)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: sb.String()}},
		StructuredContent: map[string]any{
			fieldSummary: fmt.Sprintf("Standards search for '%s': %d results.", args.Query, len(results)),
			fieldData: map[string]any{
				fieldCount:   len(results),
				fieldEntries: entries,
			},
		},
	}, nil, nil
}

func (rs *MCPRecallServer) handleGetStandard(ctx context.Context, _ *mcp.CallToolRequest, args GetStandardInput) (*mcp.CallToolResult, any, error) {
	rec, err := rs.store.Get(ctx, args.Key)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Standard not found: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	// Verify this is a standards record (domain-based, matching handleGetProject).
	if rec.Domain != memory.DomainStandards {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Key '%s' is not a standards record (domain: %s). Use 'recall' for memories or 'get' with the correct namespace.", args.Key, rec.Domain)}},
			IsError: true,
		}, nil, nil
	}

	return &mcp.CallToolResult{
		StructuredContent: map[string]any{
			fieldSummary: fmt.Sprintf("Standard '%s' retrieved.", args.Key),
			fieldData: map[string]any{
				fieldKey:       args.Key,
				fieldTitle:     rec.Title,
				fieldCategory:  rec.Category,
				fieldTags:      rec.Tags,
				fieldContent:   rec.Content,
				"source_path":  rec.SourcePath,
				"source_hash":  rec.SourceHash,
				fieldCreatedAt: rec.CreatedAt,
				fieldUpdatedAt: rec.UpdatedAt,
			},
		},
	}, nil, nil
}

func (rs *MCPRecallServer) handleDeleteStandards(ctx context.Context, _ *mcp.CallToolRequest, args DeleteStandardsInput) (*mcp.CallToolResult, any, error) {
	// ── Branch 1: Single key deletion ──
	if args.Key != "" {
		rec, err := rs.store.Get(ctx, args.Key)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: key %q not found: %v", args.Key, err)}},
				IsError: true,
			}, nil, nil
		}
		if rec.Domain != memory.DomainStandards {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: key %q belongs to domain %q, not standards", args.Key, rec.Domain)}},
				IsError: true,
			}, nil, nil
		}
		if err := rs.store.DeleteBatch(ctx, []string{args.Key}); err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error deleting standard: %v", err)}},
				IsError: true,
			}, nil, nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Deleted standard '%s'.", args.Key)}},
		}, nil, nil
	}

	// ── Branch 2: Batch key deletion ──
	if len(args.Keys) > 0 {
		var validated []string
		var skipped []string
		for _, k := range args.Keys {
			rec, err := rs.store.Get(ctx, k)
			if err != nil {
				skipped = append(skipped, k)
				continue
			}
			if rec.Domain == memory.DomainStandards {
				validated = append(validated, k)
			} else {
				skipped = append(skipped, k)
			}
		}
		if len(validated) > 0 {
			if err := rs.store.DeleteBatch(ctx, validated); err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error batch deleting standards: %v", err)}},
					IsError: true,
				}, nil, nil
			}
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "Deleted %d of %d requested standards.", len(validated), len(args.Keys))
		if len(skipped) > 0 {
			fmt.Fprintf(&sb, " Skipped %d (not found or wrong domain).", len(skipped))
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: sb.String()}},
		}, nil, nil
	}

	// ── Branch 3: Tag-based deletion ──
	if len(args.Tags) > 0 {
		query := &memory.AttributeQuery{
			Tags:         args.Tags,
			TagMatchMode: args.TagMatchMode,
		}
		found, err := rs.store.GetByAttributes(ctx, memory.DomainStandards, query)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error querying by tags: %v", err)}},
				IsError: true,
			}, nil, nil
		}
		if len(found) == 0 {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "No standards matched the specified tags."}},
			}, nil, nil
		}
		keys := make([]string, 0, len(found))
		for k := range found {
			keys = append(keys, k)
		}
		if err := rs.store.DeleteBatch(ctx, keys); err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error deleting tag-matched standards: %v", err)}},
				IsError: true,
			}, nil, nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Deleted %d standards matching tags %v.", len(keys), args.Tags)}},
		}, nil, nil
	}

	// ── Branch 4 (existing): category/package/category_number/all ──
	if !args.All && args.Category == "" && args.Package == "" && args.CategoryNumber <= 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: must specify key, keys, tags, category, package, category_number, or explicitly set 'all' to true"}},
			IsError: true,
		}, nil, nil
	}

	pkgFilter := args.Package
	catFilter := args.Category

	// category_number selects the Nth category from the overview listing. The
	// overview is category-grouped since 0005-MADR, so it resolves to a category
	// rather than a package path.
	if args.CategoryNumber > 0 {
		groups, err := rs.store.ListStandardsOverview(ctx, "")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to list standards: %w", err)
		}
		var catNames []string
		for c := range groups {
			catNames = append(catNames, c)
		}
		sort.Strings(catNames)

		if args.CategoryNumber > len(catNames) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: category_number %d is out of bounds (max %d)", args.CategoryNumber, len(catNames))}},
				IsError: true,
			}, nil, nil
		}
		catFilter = catNames[args.CategoryNumber-1]
	}

	deletedCount, err := rs.store.DeleteStandards(ctx, catFilter, pkgFilter)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error deleting standards: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Deleted %d standard records.", deletedCount)
	if args.Category != "" {
		fmt.Fprintf(&sb, " Category: %s.", args.Category)
	}
	if pkgFilter != "" {
		fmt.Fprintf(&sb, " Package: %s.", pkgFilter)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: sb.String()}},
	}, nil, nil
}
