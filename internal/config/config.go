// Package config provides functionality for the config subsystem.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

// Identity and storage defaults for the recall server.
const (
	// Name is the application name, used for config and data directory scoping.
	Name = "mcp-server-recall"
	// DefaultDBName is the directory name of the on-disk datastore.
	DefaultDBName = ".mcp_recall"
	// EnvPrefix is the prefix for configuration environment variables.
	EnvPrefix = "MCP_RECALL"

	// DefaultLogLines is the default number of lines returned by get_internal_logs.
	DefaultLogLines = 25
)

// BatchConfig holds tunable options for SaveBatch and ingest operations.
type BatchConfig struct {
	MaxBatchSize            int `mapstructure:"max_batch_size"`
	IngestInterBatchSleepMs int `mapstructure:"ingest_inter_batch_sleep_ms"`
	LoadFastWritesEnabled   int `mapstructure:"load_fast_writes_enabled"`
}

// NamespaceSchema holds the required schema rules for a given namespace.
type NamespaceSchema struct {
	RequiredKeys []string `mapstructure:"required_keys"`
}

// State holds the actual configuration values mapped to yaml.
type State struct {
	Name                 string                     `mapstructure:"name"`
	DBPath               string                     `mapstructure:"dbPath"`
	ExportDir            string                     `mapstructure:"exportDir"`
	SearchEnabled        bool                       `mapstructure:"searchEnabled"`
	SearchLimit          int                        `mapstructure:"searchLimit"`
	EncryptionKey        string                     `mapstructure:"encryptionKey"`
	DedupThreshold       float64                    `mapstructure:"dedupThreshold"`
	SafeTools            []string                   `mapstructure:"safeTools"`
	SafeToolsInternal    []string                   `mapstructure:"safeToolsInternal"`
	Batch                BatchConfig                `mapstructure:"batchsettings"`
	DefaultPurgeDays     int                        `mapstructure:"defaultpurgedays"`
	DefaultPagination    int                        `mapstructure:"default_pagination"`
	AuthorizedNamespaces []string                   `mapstructure:"authorizedNamespaces"`
	NamespaceSchemas     map[string]NamespaceSchema `mapstructure:"namespaceschemas"`
}

// Config safely wraps Viper state with an RWMutex for hot-reloads.
type Config struct {
	v            *viper.Viper
	mu           sync.RWMutex
	state        State
	Version      string
	appConfigDir string
}

// New initializes the Viper bindings, sets up OS-agnostic paths, and attaches the fsnotify hook.
func New(version string) *Config {
	v := viper.New()
	cfg := &Config{
		v:       v,
		Version: version,
	}

	v.SetEnvPrefix(EnvPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Explicit bindings to accommodate both camelCase YAML parsing and standard underscore bash environments.
	if err := v.BindEnv("encryptionKey", "MCP_RECALL_ENCRYPTION_KEY", "MCP_RECALL_ENCRYPTIONKEY"); err != nil {
		slog.Warn("failed to bind encryption key environment variable", "error", err)
	}

	appConfigDir, err := ConfigDir()
	if err != nil {
		slog.Error("failed to resolve user config directory; refusing CWD fallback", "error", err)
		appConfigDir = ""
	}
	cfg.appConfigDir = appConfigDir

	v.SetConfigName("recall")
	v.SetConfigType("yaml")
	if appConfigDir != "" {
		v.AddConfigPath(appConfigDir)
	}
	v.AddConfigPath(".")

	// Set Defaults
	v.SetDefault("name", Name)
	if def, defErr := DefaultDBPath(); defErr == nil {
		v.SetDefault("dbPath", def)
	}
	v.SetDefault("exportDir", os.TempDir())
	v.SetDefault("searchEnabled", true)
	v.SetDefault("searchLimit", 25000)
	v.SetDefault("dedupThreshold", 0.8)
	v.SetDefault("defaultpurgedays", 30)
	v.SetDefault("default_pagination", 100)

	// Batch settings defaults
	v.SetDefault("batchsettings.max_batch_size", 100)
	v.SetDefault("batchsettings.ingest_inter_batch_sleep_ms", 50)
	v.SetDefault("batchsettings.load_fast_writes_enabled", 0)

	// Baseline structured namespaces (excluding memories)
	v.SetDefault("authorizedNamespaces", []string{
		"sessions",
		"standards",
		"projects",
		"server_status",
		"dialectic_history",
		"documents",
		"modernizer_verdicts",
		"modernizer_trust",
		"madr_state",
	})

	// Default SafeTools dynamically exposed to read-only Streamable HTTP endpoint
	v.SetDefault("safeTools", []string{
		"save_to_recall",
		"search",
		"get",
		"list",
	})

	// Default SafeToolsInternal explicitly bypassing restrictions for the internal CLI
	v.SetDefault("safeToolsInternal", []string{
		"recall",
		"export_records",
		"import_records",
		"save_to_recall",
		"search",
		"get",
		"list",
		"delete",
		"prune_records",
		"forget",
		"reload_cache",
		"get_internal_logs",
		"get_metrics",
	})

	if err := v.ReadInConfig(); err != nil {
		if _, ok := errors.AsType[viper.ConfigFileNotFoundError](err); ok {
			slog.Debug("no recall.yaml config file found; relying on defaults")
		} else {
			slog.Warn("error parsing recall.yaml", "error", err)
			// A config written before the encryptionkey tag fix carries `!!null <hex>`, which
			// typed decoding rejects — silently dropping a real key and opening the store
			// unencrypted. Recover just that value; anything broader would mask genuine
			// corruption. See docs/0001-MADR-encryptionkey-yaml-tag-round-trip.md.
			if key := recoverNullTaggedKey(err, appConfigDir); key != "" {
				v.Set("encryptionKey", key)
				slog.Warn("recovered a legacy null-tagged encryptionkey; rewrite this config with `mcp-server-recall configure`",
					"path", filepath.Join(appConfigDir, "recall.yaml"))
			}
		}
	}

	// Enable AutomaticEnv
	v.AutomaticEnv()

	cfg.refreshState()

	// Enable True Hot-Reloading Sequence
	v.WatchConfig()
	var lastConfigUpdate time.Time
	var debounceMu sync.Mutex

	v.OnConfigChange(func(e fsnotify.Event) {
		debounceMu.Lock()
		if time.Since(lastConfigUpdate) < 500*time.Millisecond {
			debounceMu.Unlock()
			return
		}
		lastConfigUpdate = time.Now()
		debounceMu.Unlock()

		slog.Info("[Viper] Configuration file modified dynamically", "file", e.Name)
		cfg.refreshState()
		slog.Info("[Viper] Configuration reloaded into memory")
	})

	return cfg
}

func (c *Config) refreshState() {
	var newState State
	if err := c.v.Unmarshal(&newState); err != nil {
		slog.Error("failed to unmarshal viper configuration", "error", err)
		return
	}

	// Always enforce the binary-linked name
	newState.Name = Name

	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = newState
}

// AuthorizedNamespaces merges the static baseline with dynamic DB namespaces.
func (c *Config) AuthorizedNamespaces() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	merged := slices.Clone(c.state.AuthorizedNamespaces)

	// Read the dynamic namespaces cache if it exists
	cachePath := filepath.Join(c.appConfigDir, ".recall-dynamic-namespaces.json")
	if b, err := os.ReadFile(cachePath); err == nil {
		var dynamic []string
		if err := json.Unmarshal(b, &dynamic); err == nil {
			for _, d := range dynamic {
				if !slices.Contains(merged, d) {
					merged = append(merged, d)
				}
			}
		}
	}
	return merged
}

// Thread-safe accessors for cross-application mapping

// GetDBPath performs the GetDBPath operation.
func (c *Config) GetDBPath() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p := strings.TrimSpace(c.state.DBPath)
	if p == "" {
		def, err := DefaultDBPath()
		if err != nil {
			return ""
		}
		p = def
	}
	if !filepath.IsAbs(p) {
		if p == "" {
			return ""
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return ""
		}
		p = abs
	}
	if UnsafeDatabasePath(p) {
		return ""
	}
	return p
}

// AppConfigDir returns the application configuration directory.
func (c *Config) AppConfigDir() string {
	return c.appConfigDir
}

// ExportDir returns the export directory, falling back to os.TempDir() if unset.
func (c *Config) ExportDir() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.state.ExportDir == "" {
		return os.TempDir()
	}
	return c.state.ExportDir
}

// SearchEnabled performs the SearchEnabled operation.
func (c *Config) SearchEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state.SearchEnabled
}

// SearchLimit performs the SearchLimit operation.
func (c *Config) SearchLimit() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state.SearchLimit
}

// EncryptionKey performs the EncryptionKey operation.
func (c *Config) EncryptionKey() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state.EncryptionKey
}

// ResolveAPIPort returns the HTTP API port. Defaults to 47669, overridden by
// MCP_ENDPOINT_API_PORT if set.
func ResolveAPIPort() int {
	if val := os.Getenv("MCP_ENDPOINT_API_PORT"); val != "" {
		if p, err := strconv.Atoi(val); err == nil && p > 0 {
			return p
		}
	}
	return 47669
}

// ResolveRecallURL returns the base MCP Recall URL. Defaults to http://localhost:47669/mcp,
// overridden by MCP_REC_URL if set.
func ResolveRecallURL() string {
	if val := os.Getenv("MCP_REC_URL"); val != "" {
		return val
	}
	return fmt.Sprintf("http://localhost:%d/mcp", ResolveAPIPort())
}

// Name performs the Name operation.
func (c *Config) Name() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state.Name
}

// SafeTools performs the SafeTools operation.
func (c *Config) SafeTools() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return slices.Clone(c.state.SafeTools)
}

// SafeToolsInternal performs the SafeToolsInternal operation.
func (c *Config) SafeToolsInternal() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return slices.Clone(c.state.SafeToolsInternal)
}

// DedupThreshold performs the DedupThreshold operation.
func (c *Config) DedupThreshold() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state.DedupThreshold
}

// BatchSettings performs the BatchSettings operation.
func (c *Config) BatchSettings() BatchConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state.Batch
}

// DefaultPurgeDays performs the DefaultPurgeDays operation.
func (c *Config) DefaultPurgeDays() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state.DefaultPurgeDays
}

// NamespaceSchemas returns the configured schema definitions.
func (c *Config) NamespaceSchemas() map[string]NamespaceSchema {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Return a deep copy to prevent external mutation
	result := make(map[string]NamespaceSchema)
	for k, v := range c.state.NamespaceSchemas {
		result[k] = NamespaceSchema{
			RequiredKeys: slices.Clone(v.RequiredKeys),
		}
	}
	return result
}

// DefaultPagination performs the DefaultPagination operation.
func (c *Config) DefaultPagination() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state.DefaultPagination
}

// recoverNullTaggedKey salvages an encryptionkey that viper refused to decode because the node
// carries an explicit !!null tag alongside a string value — the shape produced by versions of
// `configure` prior to the tag fix. It returns "" unless the error is that specific decode
// failure, so genuine corruption still surfaces as a parse error rather than being papered over.
func recoverNullTaggedKey(readErr error, appConfigDir string) string {
	if readErr == nil || !strings.Contains(readErr.Error(), "!!null") {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(appConfigDir, "recall.yaml")) //nolint:gosec // wizard-managed path
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "encryptionkey:")
		if !ok {
			continue
		}
		rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), "!!null"))
		return strings.Trim(strings.TrimSpace(rest), `"'`)
	}
	return ""
}
