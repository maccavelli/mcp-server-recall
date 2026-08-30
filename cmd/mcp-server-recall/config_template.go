// Package cmd provides functionality for the cmd subsystem.
package main

// FullConfigTemplate is a curated, natively-commented YAML template representing the complete configuration Golden standard.
// Viper's automated write destroys YAML comments; writing this explicit template prevents that.
const FullConfigTemplate = `# --- General recall configuration settings --- 
# The internal network port the server listens on (used by other MCP servers like brainstorm and go-refactor).
# Currently unused: the HTTP API port is MCP_ENDPOINT_API_PORT, else 47669.
apiport: 18001
# Cosine similarity threshold (0.0 to 1.0) above which documents are considered duplicates
dedupthreshold: 0.8
# The absolute physical path where BadgerDB and Bleve persist their state to disk.
# Leave empty to use the OS default data directory:
#   Linux:   ~/.local/share/mcp-server-recall/.mcp_recall
#   macOS:   ~/Library/Application Support/mcp-server-recall/.mcp_recall
#   Windows: %%LocalAppData%%\mcp-server-recall\.mcp_recall
dbpath: ""
# A short organizational description of this memory pool
description: Cross-Session RAG Memory Architecture
# The 64-character hex key (32 bytes) used to encrypt all memory entries symmetrically at rest
encryptionkey: %q
# Default directory where database exports or internal reports are dumped
# Leave empty to use OS temp directory (Linux: /tmp, macOS: /private/tmp, Windows: %%TEMP%%)
exportdir: ""
# Internal codename mapping for the storage schema
name: recall
# A curated whitelist of MCP tools this server is permitted to expose externally
safetools:
    - save_to_recall
    - search
    - get
    - list
# A full administrative footprint of tools exposed ONLY to the internal localhost CLI
safetools_internal:
    - recall
    - export_records
    - import_records
    - save_to_recall
    - search
    - get
    - list
    - delete
    - prune_records
    - forget
    - reload_cache
    - get_internal_logs
    - get_metrics
# A curated list of baseline structured namespaces strictly authorized for dynamic routing.
# Note: The 'memories' namespace is reserved by core infrastructure and does not need to be listed here.
authorizednamespaces:
    - sessions
    - standards
    - projects
    - server_status
    - dialectic_history
    - documents
    - ecosystem
# Schema enforcement rules for dynamic namespaces.
# Defines mandatory JSON tags that must be present in the metadata when saving to a namespace.
namespaceschemas:
    documents:
        required_keys:
            - DocType
            - TargetStack
            - SourceAuthority
            - DomainContext
# Global default age limit (in days) for preserving namespaces before they are purged.
# Exempts 'memories' which never expire.
defaultpurgedays: 30
# Toggles whether hybrid database search (BM25 + Semantic/Fuzzy) is active
searchenabled: true
# Global maximum number of results returned per search query (0 = handle purely per-query limit)
searchlimit: 0
# Global default pagination limit for standard retrieval lists and searches (when limit is 0 or omitted)
default_pagination: 100

badgerdb:
    # --- I/O & Durability ---
    sync_writes: true
    verify_value_checksum: false

    # --- Data Separation & Value Log ---
    value_threshold: 4096           # 4KB — keeps only small files in LSM
    value_log_file_size: 16777216   # 16MB — safety net for vlog rotation
    value_log_max_entries: 50       # Rotate aggressively if vlog is used

    # --- Versioning ---
    num_versions_to_keep: 1
    detect_conflicts: true

    # --- Memory & Memtables ---
    memtable_size: 16777216         # 16MB
    num_memtables: 2                # Reduced from default 5 for SSH stability

    # --- Caching ---
    index_cache_size: 16777216      # 16MB
    block_cache_size: 33554432      # 32MB

    # --- LSM Tree Structure ---
    base_table_size: 8388608        # 8MB — L0 SSTable size
    base_level_size: 10485760       # 10MB — target for base compaction level
    level_size_multiplier: 10
    max_levels: 5
    block_size: 4096
    bloom_false_positive: 0.01

    # --- Compaction & Maintenance ---
    num_compactors: 2               # min 2 per BadgerDB constraint (pinned low for SSH)
    num_level_zero_tables: 5
    num_level_zero_tables_stall: 10
    compact_l0_on_close: true

    # --- Compression ---
    compression: "zstd"
    zstd_compression_level: 1       # 1=fast, 15=dense

    # --- Checksums ---
    checksum_verification_mode: 1   # 0=None, 1=OnTableRead, 2=OnBlockRead

    # --- Concurrency ---
    num_goroutines: 4
    metrics_enabled: true

batchsettings:
    max_batch_size: 100                           # Hard cap on entries per SaveBatch txn
    ingest_inter_batch_sleep_ms: 100              # Throttle between ProcessPath batches (ms)
    load_fast_writes_enabled: 0                   # 0=normal, 1=fast mode (sleep=0, doubled chunks)

bleveindex:
    rebuild_batch_size: 50                       # Docs per Bleve batch during cold-start rebuild
    # --- Scorch Engine ---
    unsafe_batch: false                           # Skip fsync on batch writes
    num_snapshots_to_keep: 1                      # Snapshot retention
    # --- Persister ---
    persister_nap_time_ms: 50                     # Sleep between persist cycles (0=immediate)
    persister_nap_under_num_files: 200            # Nap only below this file count
    num_persister_workers: 1                      # Concurrent persister goroutines
    max_size_in_memory_merge_per_worker: 0        # Max in-memory merge per worker (0=legacy)
    # --- Merge Plan ---
    max_segments_per_tier: 10                     # Threshold before forced merge
    max_segment_size: 5000                        # Max segment doc count
    tier_growth: 10.0                             # Level growth multiplier
    segments_per_merge_task: 10                   # Segments merged per operation
    floor_segment_size: 1000                      # Min segment doc count

`
