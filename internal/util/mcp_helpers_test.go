package util

import (
	"context"
	"testing"
)

func TestWithClientAndFromContext(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		client   string
		expected string
	}{
		{
			name:     "empty context returns stdio",
			ctx:      context.Background(),
			client:   "",
			expected: "stdio",
		},
		{
			name:     "valid client returns client name",
			ctx:      context.Background(),
			client:   "test-client",
			expected: "test-client",
		},
		{
			name:     "empty string client returns stdio",
			ctx:      context.Background(),
			client:   "", // WithClient("", "")
			expected: "stdio",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := tc.ctx
			// Only inject if client is non-empty or if we want to explicitly inject empty
			// Actually we will always inject using WithClient to test both functions
			if tc.name == "empty context returns stdio" {
				// don't inject
			} else {
				ctx = WithClient(ctx, tc.client)
			}

			result := ClientFromContext(ctx)
			if result != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, result)
			}
		})
	}
}
