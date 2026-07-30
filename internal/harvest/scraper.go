// Package harvest provides functionality for the harvest subsystem.
package harvest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// ScrapeWebDocument retrieves a raw HTTP target and reformats it into a HarvestResult.
// This enables boundless omnimodal datastore expansion natively indexing pure text alongside AST structures.
func ScrapeWebDocument(ctx context.Context, url string) (*HarvestResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to construct web request for %s: %w", url, err)
	}

	// Be polite. Simulate a standard agent.
	req.Header.Set("User-Agent", "MagicTools-Recall-Scraper/1.0 (CSSA Ecosystem)")

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch remote web document from %s: %w", url, err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			slog.Debug("failed to close scraper response body", "error", closeErr)
		}
	}()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("failed to retrieve document, remote server returned status: %s", resp.Status)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	contentStr := string(bodyBytes)

	// In the absence of a strict DOM to Markdown engine, gracefully limit size and just index it contextually
	docContent := contentStr
	if len(docContent) > 200000 { // Max text truncating ~200kb
		docContent = docContent[:200000] + "\n...[truncated by recall web limits]..."
	}

	// Generate structural checksum
	hash := sha256.Sum256([]byte(docContent))
	checksum := hex.EncodeToString(hash[:])

	// Enforce the AST signature mapping so the database absorbs this organically just like Go code
	sym := HarvestedSymbol{
		PkgPath:    url,
		Name:       "WebDocument",
		SymbolType: "HTML",
		Summary:    fmt.Sprintf("Raw web extraction from %s via Heuristic Targeter", url),
		Signature:  fmt.Sprintf("HTTP GET -> %s", url),
		Doc:        docContent,
	}

	res := &HarvestResult{
		Symbols:     []HarvestedSymbol{sym},
		Checksum:    checksum,
		PackageDocs: map[string]string{url: docContent},
	}

	return res, nil
}
