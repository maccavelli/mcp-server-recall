package config

import (
	"testing"

	"github.com/spf13/viper"
)

func TestConfig_AllAccessors(t *testing.T) {
	viper.Reset()
	t.Setenv("HOME", t.TempDir()) // Isolate from host recall.yaml
	c := New("v1.2.3")

	// String accessors
	if c.Name() == "" {
		t.Error("expected non-empty server name")
	}
	if c.ExportDir() == "" {
		t.Error("expected non-empty export dir")
	}
	_ = c.EncryptionKey() // may be empty — just verify no panic

	// Numeric accessors
	if c.SearchLimit() <= 0 {
		t.Errorf("expected positive search limit, got %d", c.SearchLimit())
	}
	if ResolveAPIPort() <= 0 {
		t.Errorf("expected positive API port, got %d", ResolveAPIPort())
	}
	if c.DedupThreshold() <= 0 {
		t.Errorf("expected positive dedup threshold, got %f", c.DedupThreshold())
	}

	// Boolean accessors
	_ = c.SearchEnabled()
	_ = c.HarvestDisableDrift()

	// Slice accessors — verify they return copies, not nil
	dirs := c.ExcludeDirs()
	if dirs == nil {
		t.Error("expected non-nil exclude dirs slice")
	}
	tools := c.SafeTools()
	if len(tools) == 0 {
		t.Error("expected at least one safe tool configured")
	}

	// Batch settings
	b := c.BatchSettings()
	if b.MaxBatchSize <= 0 {
		t.Errorf("expected positive max batch size, got %d", b.MaxBatchSize)
	}
	if b.HarvestChunkSize <= 0 {
		t.Errorf("expected positive harvest chunk size, got %d", b.HarvestChunkSize)
	}
	if b.HarvestInterBatchSleepMs < 0 {
		t.Error("expected non-negative harvest inter batch sleep")
	}

	// DB path must be absolute after accessor processing
	dbPath := c.GetDBPath()
	if dbPath == "" {
		t.Error("expected non-empty db path")
	}

	// Missing accessors tests
	_ = c.AppConfigDir()
	_ = c.DefaultPurgeDays()
	_ = c.DefaultPagination()
	if ResolveRecallURL() == "" {
		t.Error("expected non-empty recall url")
	}
	if len(c.AuthorizedNamespaces()) == 0 {
		t.Error("expected non-empty authorized namespaces")
	}
	if len(c.SafeToolsInternal()) == 0 {
		t.Error("expected non-empty internal safe tools")
	}
	schemas := c.NamespaceSchemas()
	if schemas == nil {
		t.Error("expected non-nil namespace schemas map")
	}
}

func TestConfig_ExcludeDirs_IsCopy(t *testing.T) {
	c := New("test")
	dirs1 := c.ExcludeDirs()
	dirs2 := c.ExcludeDirs()
	// Mutate dirs1 — should not affect dirs2
	if len(dirs1) > 0 {
		dirs1[0] = "MUTATED"
		if dirs2[0] == "MUTATED" {
			t.Error("ExcludeDirs() returned same slice reference — unsafe for concurrent use")
		}
	}
}

func TestConfig_SafeTools_IsCopy(t *testing.T) {
	c := New("test")
	tools1 := c.SafeTools()
	if len(tools1) > 0 {
		tools1[0] = "MUTATED"
		tools2 := c.SafeTools()
		if len(tools2) > 0 && tools2[0] == "MUTATED" {
			t.Error("SafeTools() returned same slice reference — unsafe for concurrent use")
		}
	}
}

func TestConfig_Version(t *testing.T) {
	c := New("v9.8.7")
	if c.Version != "v9.8.7" {
		t.Errorf("expected version v9.8.7, got %s", c.Version)
	}
}
