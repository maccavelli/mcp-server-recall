package config

import (
	"testing"

	"github.com/spf13/viper"
)

func TestConfig_Defaults(t *testing.T) {
	viper.Reset()
	t.Setenv("HOME", t.TempDir())
	c := New("test-server")

	_ = c.DedupThreshold()
	b := c.BatchSettings()
	if b.MaxBatchSize <= 0 {
		t.Errorf("expected valid batch size")
	}

	_ = c.ExportDir()
	_ = c.EncryptionKey()
	_ = c.HarvestDisableDrift()
}

// Removed legacy tests
