package config

import (
	"testing"

	"os"
	"path/filepath"
	"strings"

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

// TestConfig_RecoversNullTaggedKey covers configs written before the encryptionkey tag fix,
// which carry `encryptionkey: !!null <hex>`. Typed decoding rejects that node, so without
// recovery the key is silently dropped and the store opens unencrypted.
func TestConfig_RecoversNullTaggedKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir: %v", err)
	}
	dir := filepath.Join(base, Name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	key := strings.Repeat("c", 64)
	legacy := "name: " + Name + "\nencryptionkey: !!null " + key + "\n"
	if err := os.WriteFile(filepath.Join(dir, "recall.yaml"), []byte(legacy), 0o600); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	if got := New("test-recover").EncryptionKey(); got != key {
		t.Errorf("legacy null-tagged key not recovered:\n  got  %q\n  want %q", got, key)
	}
}
