// Package server provides functionality for the server subsystem.
package server

import (
	"github.com/maccavelli/mcp-server-recall/internal/memory"
	"github.com/maccavelli/mcp-server-recall/internal/util"
)

// DeleteMemoriesInput defines the DeleteMemoriesInput structure.
type DeleteMemoriesInput struct {
	util.UniversalBaseInput

	Key      string `json:"key"`
	Category string `json:"category,omitempty,omitzero"`
	All      bool   `json:"all,omitempty,omitzero"`
}

// ListProjectCategoriesInput defines the ListProjectCategoriesInput structure.
type ListProjectCategoriesInput struct {
	util.UniversalBaseInput

	Package    string `json:"package,omitempty,omitzero"`
	SymbolType string `json:"symbol_type,omitempty,omitzero"`
}

// SearchProjectsInput defines the SearchProjectsInput structure.
type SearchProjectsInput struct {
	util.UniversalBaseInput

	Query        string   `json:"query,omitempty,omitzero"`
	Package      string   `json:"package,omitempty,omitzero"`
	SymbolType   string   `json:"symbol_type,omitempty,omitzero"`
	Interface    string   `json:"interface,omitempty,omitzero"`
	Receiver     string   `json:"receiver,omitempty,omitzero"`
	Domain       string   `json:"domain,omitempty,omitzero"`
	Limit        int      `json:"limit,omitempty,omitzero"`
	KeyPrefix    string   `json:"key_prefix,omitempty,omitzero"`
	KeySuffix    string   `json:"key_suffix,omitempty,omitzero"`
	Tags         []string `json:"tags,omitempty,omitzero"`
	TagMatchMode string   `json:"tag_match_mode,omitempty,omitzero"`
}

// GetProjectInput defines the GetProjectInput structure.
type GetProjectInput struct {
	util.UniversalBaseInput

	Key string `json:"key"`
}

// DeleteProjectsInput defines the DeleteProjectsInput structure.
type DeleteProjectsInput struct {
	util.UniversalBaseInput

	Key            string   `json:"key,omitempty,omitzero"`
	Keys           []string `json:"keys,omitempty,omitzero"`
	Tags           []string `json:"tags,omitempty,omitzero"`
	TagMatchMode   string   `json:"tag_match_mode,omitempty,omitzero"`
	Category       string   `json:"category,omitempty,omitzero"`
	Package        string   `json:"package,omitempty,omitzero"`
	CategoryNumber int      `json:"category_number,omitempty,omitzero"`
	All            bool     `json:"all,omitempty,omitzero"`
}

// ListStandardsCategoriesInput defines the ListStandardsCategoriesInput structure.
type ListStandardsCategoriesInput struct {
	util.UniversalBaseInput

	Package    string `json:"package,omitempty,omitzero"`
	SymbolType string `json:"symbol_type,omitempty,omitzero"`
}

// SearchStandardsInput defines the SearchStandardsInput structure.
type SearchStandardsInput struct {
	util.UniversalBaseInput

	Query        string   `json:"query,omitempty,omitzero"`
	Package      string   `json:"package,omitempty,omitzero"`
	SymbolType   string   `json:"symbol_type,omitempty,omitzero"`
	Interface    string   `json:"interface,omitempty,omitzero"`
	Receiver     string   `json:"receiver,omitempty,omitzero"`
	Domain       string   `json:"domain,omitempty,omitzero"`
	Limit        int      `json:"limit,omitempty,omitzero"`
	KeyPrefix    string   `json:"key_prefix,omitempty,omitzero"`
	KeySuffix    string   `json:"key_suffix,omitempty,omitzero"`
	Tags         []string `json:"tags,omitempty,omitzero"`
	TagMatchMode string   `json:"tag_match_mode,omitempty,omitzero"`
	MetadataOnly bool     `json:"metadata_only,omitempty,omitzero"`
}

// GetStandardInput defines the GetStandardInput structure.
type GetStandardInput struct {
	util.UniversalBaseInput

	Key string `json:"key"`
}

// DeleteStandardsInput defines the DeleteStandardsInput structure.
type DeleteStandardsInput struct {
	util.UniversalBaseInput

	Key            string   `json:"key,omitempty,omitzero" jsonschema:"Delete a single standard by its exact key."`
	Keys           []string `json:"keys,omitempty,omitzero" jsonschema:"Delete multiple standards by exact keys (batch)."`
	Tags           []string `json:"tags,omitempty,omitzero" jsonschema:"Delete standards matching these tags."`
	TagMatchMode   string   `json:"tag_match_mode,omitempty,omitzero" jsonschema:"'all' (AND) or 'any' (OR). Defaults to 'all'."`
	Category       string   `json:"category,omitempty,omitzero"`
	Package        string   `json:"package,omitempty,omitzero"`
	CategoryNumber int      `json:"category_number,omitempty,omitzero"`
	All            bool     `json:"all,omitempty,omitzero"`
}

// ListCategoriesInput defines the ListCategoriesInput structure.
type ListCategoriesInput struct {
	util.UniversalBaseInput

	Filename string `json:"filename,omitempty,omitzero"`
}

// IngestFilesInput defines the IngestFilesInput structure.
type IngestFilesInput struct {
	util.UniversalBaseInput

	Path      string `json:"path"`
	Namespace string `json:"namespace,omitempty,omitzero"`
}

// ContextVacuumInput defines the ContextVacuumInput structure.
type ContextVacuumInput struct {
	util.UniversalBaseInput

	Namespace        string  `json:"namespace,omitempty,omitzero"`
	TargetOutcome    string  `json:"target_outcome,omitempty,omitzero"`
	FlattenThreshold int     `json:"flatten_threshold,omitempty,omitzero"`
	DaysOld          int     `json:"days_old,omitempty,omitzero"`
	DedupThreshold   float64 `json:"dedup_threshold,omitempty,omitzero"`
	Category         string  `json:"category,omitempty,omitzero"`
	ReportOnly       bool    `json:"report_only,omitempty,omitzero"`
}

// RememberInput defines the RememberInput structure.
type RememberInput struct {
	util.UniversalBaseInput

	Title          *string              `json:"title,omitempty,omitzero"`
	Key            *string              `json:"key,omitempty,omitzero"`
	Value          *string              `json:"value,omitempty,omitzero"`
	Category       *string              `json:"category,omitempty,omitzero"`
	Tags           *[]string            `json:"tags,omitempty,omitzero"`
	DedupThreshold *float64             `json:"dedup_threshold,omitempty,omitzero"`
	Entries        *[]memory.BatchEntry `json:"entries,omitempty,omitzero"`
}

// SaveToRecallInput defines the SaveToRecallInput structure.
type SaveToRecallInput struct {
	util.UniversalBaseInput

	Namespace    string  `json:"namespace,omitempty,omitzero" jsonschema:"Target domain. One of: sessions, server_status, dialectic_history, standards, projects, ecosystem, modernizer_verdicts, modernizer_trust, madr_state. Defaults to standards. 'memories' is NOT valid here — use 'remember' tool instead."`
	Key          string  `json:"key,omitempty,omitzero" jsonschema:"Client-provided storage key. If empty, auto-generated from server_id/session_id matrix for sessions/server_status, or from server_id:namespace:timestamp for other namespaces."`
	Category     string  `json:"category,omitempty,omitzero" jsonschema:"Optional semantic category for the record. Defaults to server_id if not provided."`
	ServerID     string  `json:"server_id"`
	ProjectID    string  `json:"project_id"`
	Outcome      string  `json:"outcome"`
	SessionID    *string `json:"session_id,omitempty,omitzero" jsonschema:"Session correlation ID,optional"`
	Model        string  `json:"model,omitempty,omitzero"`
	TokenSpend   int     `json:"token_spend,omitempty,omitzero"`
	TraceContext string  `json:"trace_context,omitempty,omitzero"`
	StateData    string  `json:"state_data"`
}

// RecallInput defines the RecallInput structure.
type RecallInput struct {
	util.UniversalBaseInput

	Key   *string   `json:"key,omitempty,omitzero"`
	Keys  *[]string `json:"keys,omitempty,omitzero"`
	Count *int      `json:"count,omitempty,omitzero"`
}

// SearchMemoriesInput defines the SearchMemoriesInput structure.
type SearchMemoriesInput struct {
	util.UniversalBaseInput

	Query string `json:"query"`
	Tag   string `json:"tag"`
	Limit int    `json:"limit"`
}

// SearchSessionsInput defines the SearchSessionsInput structure.
type SearchSessionsInput struct {
	util.UniversalBaseInput

	Domain       string `json:"domain,omitempty,omitzero"`
	Query        string `json:"query,omitempty,omitzero"`
	ProjectID    string `json:"project_id,omitempty,omitzero"`
	ServerID     string `json:"server_id,omitempty,omitzero"`
	Outcome      string `json:"outcome,omitempty,omitzero"`
	TraceContext string `json:"trace_context,omitempty,omitzero"`
	Limit        int    `json:"limit,omitempty,omitzero"`
}

// ListMemoriesInput defines the ListMemoriesInput structure.
type ListMemoriesInput struct {
	util.UniversalBaseInput
}

// ListSessionsInput defines the ListSessionsInput structure.
type ListSessionsInput struct {
	util.UniversalBaseInput

	Domain          string `json:"domain,omitempty,omitzero"`
	ProjectID       string `json:"project_id,omitempty,omitzero"`
	ServerID        string `json:"server_id,omitempty,omitzero"`
	Outcome         string `json:"outcome,omitempty,omitzero"`
	TraceContext    string `json:"trace_context,omitempty,omitzero"`
	Limit           int    `json:"limit,omitempty,omitzero"`
	TruncateContent bool   `json:"truncate_content,omitempty,omitzero"`
}

// GetSessionsInput defines the GetSessionsInput structure.
type GetSessionsInput struct {
	util.UniversalBaseInput

	Domain    string  `json:"domain,omitempty,omitzero"`
	Key       string  `json:"key,omitempty,omitzero"`
	SessionID *string `json:"session_id,omitempty,omitzero" jsonschema:"Match specific session ID,optional"`
}

// DeleteSessionsInput defines the DeleteSessionsInput structure.
type DeleteSessionsInput struct {
	util.UniversalBaseInput

	Domain    string  `json:"domain,omitempty,omitzero"`
	Key       string  `json:"key,omitempty,omitzero"`
	SessionID *string `json:"session_id,omitempty,omitzero" jsonschema:"Match specific session ID,optional"`
	All       bool    `json:"all,omitempty,omitzero"`
}

// GetMetricsInput defines the GetMetricsInput structure.
type GetMetricsInput struct {
	util.UniversalBaseInput
}

// ReloadCacheInput defines the ReloadCacheInput structure.
type ReloadCacheInput struct {
	util.UniversalBaseInput
}

// ForgetInput defines the ForgetInput structure.
type ForgetInput struct {
	util.UniversalBaseInput
	Key  string   `json:"key,omitempty,omitzero"`
	Keys []string `json:"keys,omitempty,omitzero"`
}

// ExportMemoriesInput defines the ExportMemoriesInput structure.
type ExportMemoriesInput struct {
	util.UniversalBaseInput

	Filename string `json:"filename"`
}

// ImportMemoriesInput defines the ImportMemoriesInput structure.
type ImportMemoriesInput struct {
	util.UniversalBaseInput

	Filename string `json:"filename"`
}
