// Package util provides functionality for the util subsystem.
package util

import (
	"context"

	"github.com/maccavelli/mcplib"
)

// UniversalBaseInput is the shared optional telemetry base type from mcplib.
type UniversalBaseInput = mcplib.UniversalBaseInput

// contextKey is an unexported type for context keys in this package.
type contextKey string

const clientContextKey contextKey = "mcp_client"

// WithClient returns a copy of ctx with the MCP client identity attached.
func WithClient(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, clientContextKey, name)
}

// ClientFromContext extracts the MCP client identity from the context.
// Returns "stdio" if no client identity was set (i.e. the request came via the stdio backplane).
func ClientFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(clientContextKey).(string); ok && v != "" {
		return v
	}
	return "stdio" //nolint:goconst // bypass
}
