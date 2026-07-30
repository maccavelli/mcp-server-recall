package harvest

import (
	"context"
	"testing"
)

func TestResolveSource(t *testing.T) {
	ctx := context.Background()

	tests := []string{
		"fmt",
		"github.com/pkg/errors",
		"https://pkg.go.dev/github.com/gin-gonic/gin/binding",
	}

	for _, tt := range tests {
		t.Run(tt, func(t *testing.T) {
			res, err := ResolveSource(ctx, tt)
			if err != nil {
				t.Fatalf("Failed to resolve %q: %v", tt, err)
			}
			if res == "" || res == tt {
				t.Errorf("ResolveSource(%q) returned an unresolved string: %q", tt, res)
			}
			t.Logf("Successfully resolved %q to %q\n", tt, res)
		})
	}
}
