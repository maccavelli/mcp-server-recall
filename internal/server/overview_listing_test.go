package server

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestListReturnsSaveToRecallRecords is the 0005-PLAN Phase 7 acceptance test.
// Before the overview rewrite, list standards and list projects scanned only the
// harvested-category indexes and required a pkg: key prefix, so neither could
// ever return a record written by save_to_recall. Both now do.
func TestListReturnsSaveToRecallRecords(t *testing.T) {
	srv, _, cleanup := createTestServer(t)
	defer cleanup()
	ctx := context.Background()

	// Write exactly what save_to_recall writes.
	if _, _, err := srv.handleSaveToRecall(ctx, makeReq(`{}`), SaveToRecallInput{
		Namespace: "standards", Key: "STD-TEST-001", Category: "Logging",
		ServerID: "srv", StateData: `{"rule":"wrap errors"}`,
	}); err != nil {
		t.Fatalf("save standards: %v", err)
	}
	if _, _, err := srv.handleSaveToRecall(ctx, makeReq(`{}`), SaveToRecallInput{
		Namespace: "projects", Key: "PRJ-TEST-001", Category: "Roadmap",
		ServerID: "srv", StateData: `{"q":"3"}`,
	}); err != nil {
		t.Fatalf("save projects: %v", err)
	}

	res, _, err := srv.handleListStandardsCategories(ctx, makeReq(`{}`), ListStandardsCategoriesInput{})
	if err != nil {
		t.Fatalf("list standards: %v", err)
	}
	out := renderText(res)
	t.Logf("list standards output:\n%s", out)
	if !strings.Contains(out, "STD-TEST-001") || !strings.Contains(out, "Logging") {
		t.Errorf("standards listing missing the save_to_recall record:\n%s", out)
	}
	if strings.Contains(out, "PRJ-TEST-001") {
		t.Errorf("domain leak: projects record in standards listing:\n%s", out)
	}

	pres, _, err := srv.handleListProjectCategories(ctx, makeReq(`{}`), ListProjectCategoriesInput{})
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	pout := renderText(pres)
	t.Logf("list projects output:\n%s", pout)
	if !strings.Contains(pout, "PRJ-TEST-001") || !strings.Contains(pout, "Roadmap") {
		t.Errorf("projects listing missing the save_to_recall record:\n%s", pout)
	}
}

func renderText(res *mcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
