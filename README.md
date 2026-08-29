> **Mirror notice:** This repository is a one-way published export of a
> privately hosted project. History is squashed into sync snapshots, and pull
> requests cannot be merged here directly — open an issue instead. Changes
> land in the private source and are re-exported.

<!-- markdownlint-disable MD013 MD060 MD033 -->

# mcp-server-recall

A high-performance Model Context Protocol (MCP) sub-server for long-term project memory, semantic search, Go package harvesting, and codebase indexing. Part of the MagicTools Intelligence Suite.

---

## Overview

`mcp-server-recall` is the persistent memory, metadata cache, and knowledge graph layer for AI agents. It combines a BadgerDB key-value store (with native Zstd compression) with a Bleve full-text search index to provide hybrid BM25 + semantic recall across nine isolated namespaces.

### Core Capabilities

| Feature | Description |
|---|---|
| **Hybrid Search** | Combines BM25 keyword matching (via Bleve) and semantic metadata search for maximum recall. |
| **9 Isolated Namespaces** | Segregated domains for memories, sessions, standards, projects, dialectic history, server status, modernizer verdicts, modernizer trust, and MADR states. |
| **Go Package Harvesting** | Deep AST + type-system extraction from local and remote Go packages via the Go toolchain. |
| **Universal Persist & Retrieve** | Consolidated multi-key batch operations for `get`, `remember`, `recall`, and `forget`. |
| **Encrypted at Rest** | AES-256-GCM encryption on all database records. |
| **Localhost HTTP API** | Localhost-bound Streamable HTTP endpoint for internal CLI tools alongside the stdio backplane. |
| **Dual-Path Telemetry & TUI** | Real-time observability dashboard (`dash`) powered by UDP telemetry with a disk-polling fallback. |
| **Path Sandboxing** | Strict path isolation for import/export routines preventing host directory traversal. |
| **Consolidated CLI** | Unified setup under `configure` featuring interactive `pterm` prompts. |

---

## ⚠️ Prerequisites: Go Toolchain Required

> **The `harvest` subsystem — which provides Go package indexing and AST extraction — requires the Go toolchain to be installed on the host system.**
>
> Download and install Go from the official archive: **[https://go.dev/dl/](https://go.dev/dl/)**
>
> Minimum required version: **Go 1.26+** (Go 1.26.5 recommended)

The server binary itself runs without Go. However, the following CLI commands and MCP tools **will fail** unless the `go` binary is resolvable at runtime:

- `harvest projects` / `harvest standards` — calls `go list`, `go doc`, and `golang.org/x/tools/go/packages` internally
- Any `recall` tool call that triggers a harvest

### MCP_GO_BIN_PATH — Explicit Binary Override

When `mcp-server-recall` runs as a subprocess under an IDE orchestrator, it often inherits a **stripped `PATH`** with no `go` binary available. The recommended fix is to set `MCP_GO_BIN_PATH` explicitly in your IDE's MCP server configuration:

```json
{
  "mcpServers": {
    "recall": {
      "command": "/home/youruser/.local/bin/mcp-server-recall",
      "args": ["serve"],
      "env": {
        "HOME": "/home/youruser",
        "MCP_GO_BIN_PATH": "/home/youruser/sdk/go1.26.5/bin/go"
      }
    }
  }
}
```

See [Go Toolchain Resolution](#go-toolchain-resolution) for the full resolution order.

---

## Quick Start

### Step 1: Place the Binary

#### Linux

```bash
mv mcp-server-recall ~/.local/bin/mcp-server-recall
chmod +x ~/.local/bin/mcp-server-recall
```

#### macOS

```bash
mv mcp-server-recall /usr/local/bin/mcp-server-recall
chmod +x /usr/local/bin/mcp-server-recall
```

#### Windows (PowerShell)

```powershell
New-Item -ItemType Directory -Force -Path "$env:LOCALAPPDATA\Programs\recall"
Move-Item mcp-server-recall.exe "$env:LOCALAPPDATA\Programs\recall\mcp-server-recall.exe"
$currentPath = [Environment]::GetEnvironmentVariable("Path", "User")
[Environment]::SetEnvironmentVariable("Path", "$currentPath;$env:LOCALAPPDATA\Programs\recall", "User")
```

---

### Step 2: Initialize Configuration

`mcp-server-recall` features a consolidated configuration sequence. Initialize the configuration directory, databases, and default settings using:

```bash
mcp-server-recall configure --force
```

*(Note: `init` is registered as a direct CLI alias to `configure` for backward compatibility).*

---

### Step 3: Vault Encryption Key

```bash
mcp-server-recall configure
```

Launches an interactive wizard powered by `pterm` to generate a secure AES-256-GCM encryption key or paste an existing one. For non-interactive setup, set `RECALL_ENCRYPTION_KEY` or `MCP_RECALL_ENCRYPTION_KEY` to a 64-character hex string (32 bytes). Pass `--allow-unencrypted` only to intentionally disable encryption when a key already exists.

---

### Step 4: Configure Your IDE

> **⚠️ Production Note:** `mcp-server-recall` is designed to run as a downstream node behind the **`magictools` orchestrator**. In production, configure only `magictools` in your IDE. The configs below are for standalone testing.

You **must** pass the `serve` argument when configuring any IDE integration.

#### Antigravity (Google DeepMind)

| OS | Configuration File |
|---|---|
| Linux / macOS | `~/.gemini/antigravity/mcp_config.json` |
| Windows | `%USERPROFILE%\.gemini\antigravity\mcp_config.json` |

```json
{
  "mcpServers": {
    "recall": {
      "command": "/home/youruser/.local/bin/mcp-server-recall",
      "args": ["serve"],
      "env": {
        "HOME": "/home/youruser"
      }
    }
  }
}
```

#### Visual Studio Code (GitHub Copilot / Native MCP)

| OS | Configuration File |
|---|---|
| Linux | `~/.config/Code/User/mcp.json` |
| macOS | `~/Library/Application Support/Code/User/mcp.json` |
| Windows | `%APPDATA%\Code\User\mcp.json` |

```json
{
  "mcpServers": {
    "recall": {
      "command": "/home/youruser/.local/bin/mcp-server-recall",
      "args": ["serve"],
      "env": {
        "HOME": "/home/youruser"
      }
    }
  }
}
```

#### Claude Desktop

| OS | Configuration File |
|---|---|
| macOS | `~/Library/Application Support/Claude/claude_desktop_config.json` |
| Windows | `%APPDATA%\Claude\claude_desktop_config.json` |

```json
{
  "mcpServers": {
    "recall": {
      "command": "/usr/local/bin/mcp-server-recall",
      "args": ["serve"],
      "env": {
        "HOME": "/Users/youruser"
      }
    }
  }
}
```

---

## CLI Commands Reference

### `configure` (alias: `init`)

Unifies the configuration flow. Creates or rewrites `recall.yaml`, creates the OS data directory, and materializes an empty Badger store (`MANIFEST`).

```bash
mcp-server-recall configure [flags]
```

| Flag | Short | Description |
|---|---|---|
| `--force` | `-f` | Overwrites configuration and resets configuration settings to default. |
| `--allow-unencrypted` | | Permit a non-interactive run to disable encryption on a config that already has a key. |

Non-interactive key input uses `RECALL_ENCRYPTION_KEY`, then `MCP_RECALL_ENCRYPTION_KEY`, then `MCP_RECALL_ENCRYPTIONKEY` (first non-empty wins). The value must be 64 hexadecimal characters.

---

### `serve`

Starts the stdio JSON-RPC MCP server. Also binds a localhost-only HTTP Streamable API on `apiport` (default: `47669`) for CLI tools. Features process singleton locking and graceful `SIGTERM` draining.

```bash
mcp-server-recall serve
```

---

### `dash`

Launches the TUI observability dashboard. Connects to `serve` via localhost UDP telemetry (`49156–49160`) for real-time (500ms) updates, falling back to reading the telemetry ring buffer file on disk every 10 seconds.

```bash
mcp-server-recall dash
```

*Dashboard Tabs: Summary, Semantic Search Engine, Memory & GC, Taxonomy & AST Pipeline, RPC & Gateway Analytics, Network Operations, Security, and Event Log.*

---

### `harvest`

Harvests codebase AST details. Requires Go toolchain.

#### `harvest standards`

Harvests a package (remote or stdlib) into `standards`:

```bash
mcp-server-recall harvest standards encoding/json
```

#### `harvest projects`

Harvests local directories into `projects`:

```bash
mcp-server-recall harvest projects .
```

---

### `export`

Streams the database into a JSONL backup. Enforces path sandboxing.

```bash
mcp-server-recall export [filepath]
```

---

### `import`

Restores from a JSONL backup. Enforces path sandboxing.

```bash
mcp-server-recall import [filepath]
```

---

### `prune`

Purges session and temporary records older than N days (relying on `defaultpurgedays`, default 30).

```bash
mcp-server-recall prune [days]
```

---

### `purge`

Destructively clears all BadgerDB + Bleve data.

```bash
mcp-server-recall purge [--force]
```

---

## Path Sandboxing & Security Boundaries

For file system operations (`export`, `import` CLI commands and their corresponding MCP tools `export_records`, `import_records`), `mcp-server-recall` enforces **Server-Side Path Sandboxing**:

- All target file paths are resolved to absolute paths on the client side before submission.
- The server validates that the destination path sits strictly inside the configured `exportDir` (defaults to the OS temporary directory) or the user's cache directory (`os.UserCacheDir()`).
- Path traversal sequences (e.g. `../../`) that resolve outside these directories trigger a validation error:
  `Path sandboxing violation: <path> is outside allowed export directories`

---

## Isolated Database Namespaces

The persistent layer separates metadata into 9 distinct namespaces:

1. **`memories`**: Unstructured thoughts and conversational chat snippets (read/write).
2. **`standards`**: External Go package APIs, function signatures, structures, and documentation harvested from standard or external libraries.
3. **`sessions`**: Agent execution history, tracking tool runs, and outcomes.
4. **`projects`**: Local workspace AST code elements and extracted symbols.
5. **`dialectic_history`**: Conversational context history logs.
6. **`server_status`**: Telemetry and server diagnostic status logs.
7. **`modernizer_verdicts`**: Socratic architecture modernization gate outcomes.
8. **`modernizer_trust`**: Safety metrics, analysis, and trust scores for codebase adjustments.
9. **`madr_state`**: Markdown Architectural Decision Records representing approved designs.

---

## MCP Tools Reference

All description fields feature standardized `[DIRECTIVE: ...]` prefixes and isolated `Keywords: ...` declarations to improve agent routing efficiency and prevent prompt leaks.

### Universal Persistence

| Tool | Parameters | Description |
|---|---|---|
| `save_to_recall` | `namespace`, `server_id`, `state_data`, `key` (optional), `category` (optional), `project_id` (optional), `outcome` (optional), `trace_context` (optional) | **[DIRECTIVE: JSON Blob Writer]** Serializes state blobs to any namespace *except* `memories`. Valid namespaces: `sessions`, `server_status`, `dialectic_history`, `standards`, `projects`, `ecosystem`, `modernizer_verdicts`, `modernizer_trust`, `madr_state`. |
| `get` | `namespace`, `key` (optional), `keys` (optional), `session_id` (optional) | **[DIRECTIVE: Precision Payload Retriever]** Retrieves structured JSON data for a single `key` or performs a **batch retrieval** using the `keys` string array. Namespace-restricted. |
| `search` | `namespace`, `query`, `limit` (optional), `project_id` (optional) | **[DIRECTIVE: Fuzzy Semantic Scanner]** Searches the hybrid Bleve index across the specified namespace using BM25 query strings. |
| `list` | `namespace`, `limit` (optional) | **[DIRECTIVE: String Array Generator]** Returns flat lists of exact URIs or namespaces. |
| `delete` | `namespace`, `key`, `all` (optional) | **[DIRECTIVE: Targeted URI Destructor]** Hard-deletes records from non-memory namespaces. |
| `update_in_recall` | `namespace`, `key`, `replacement_chunks` (array), `new_key` (optional), `new_category` (optional), `new_metadata` (optional) | **[DIRECTIVE: In-Place Record Mutator]** Surgically replaces content or updates metadata of an existing record natively without downloading its full payload. |

### Memory Tools

| Tool | Parameters | Description |
|---|---|---|
| `remember` | `key`, `value`, `title` (optional), `tags` (optional), `entries` (optional) | **[DIRECTIVE: Dialogue Ingestion]** Commits single thoughts or multiple `entries` into the `memories` namespace. |
| `recall` | `key` (optional), `keys` (optional), `count` (optional) | **[DIRECTIVE: Historical Interaction Fetcher]** Retrieves single, batch (`keys`), or recent (`count`) memories from the `memories` namespace. |
| `forget` | `key` (optional), `keys` (optional) | **[DIRECTIVE: Chat Transcript Eraser]** Deletes one or multiple memory keys from the `memories` namespace. |

### Operations Tools

| Tool | Description |
|---|---|
| `get_metrics` | **[DIRECTIVE: Recall System Telemetry Monitor]** Returns virtual memory, CPU usage, GC, active goroutines, and namespace sizes. |
| `get_internal_logs` | **[SERVER: recall] RECALL LOG INSPECTOR** Retrieves diagnostic logs from the server ring buffer. |
| `reload_cache` | **[DIRECTIVE: Recall Index Reconstruction]** Synchronizes Bleve indices with BadgerDB. |
| `prune_records` | **[DIRECTIVE: Recall TTL Garbage Collection]** Cleans up entries exceeding the TTL threshold. |
| `export_records` | **[DIRECTIVE: Recall Outbound File Archiver]** Dumps all records to a JSONL file (sandboxed). |
| `import_records` | **[DIRECTIVE: Recall Inbound Recovery Loader]** Imports a JSONL dump (sandboxed). |
| `ingest_files` | **[DIRECTIVE: Documentation Library Importer]** Recursively indexes a directory into `memories`. |

---

## Go Toolchain Resolution

For `harvest` operations, the Go compiler binary is resolved using the following order:

1. `MCP_GO_BIN_PATH` environment variable (highest priority override).
2. `$GOROOT/bin/go` (if `GOROOT` is set).
3. Common SDK paths (`~/sdk/go*/bin/go`, `~/.local/go/bin/go`, `~/go/bin/go`, `/usr/local/go/bin/go`).
4. Standard PATH lookup (`exec.LookPath("go")`).

---

## Configuration Reference (`recall.yaml`)

Default config path (OS-specific):

* Linux: `~/.config/mcp-server-recall/recall.yaml`
* macOS: `~/Library/Application Support/mcp-server-recall/recall.yaml`
* Windows: `%APPDATA%\mcp-server-recall\recall.yaml`

```yaml
# Internal HTTP API port (used by CLI tools and internal localhost HTTP clients)
apiport: 47669

# Cosine similarity threshold for semantic deduplication (0.0–1.0)
dedupthreshold: 0.8

# Absolute path for BadgerDB + Bleve storage. Empty means the OS data directory:
#   Linux:   ~/.local/share/mcp-server-recall/.mcp_recall
#   macOS:   ~/Library/Application Support/mcp-server-recall/.mcp_recall
#   Windows: %LocalAppData%\mcp-server-recall\.mcp_recall
dbpath: ""

# AES-256-GCM encryption key (64-character hex / 32 bytes). Empty = encryption disabled.
encryptionkey: ""

# Directory for JSONL exports. Defaults to OS temp directory.
exportdir: ""

# Tools exposed over the external HTTP Streamable endpoint
safetools:
    - save_to_recall
    - search
    - get
    - list

# Tools exposed over localhost (full administrative set)
safetools_internal:
    - recall
    - export_records
    - import_records
    - save_to_recall
    - search
    - get
    - list
    - harvest
    - delete
    - prune_records
    - forget
    - reload_cache
    - get_internal_logs
    - get_metrics

# Days to retain session records before prune_records removes them
defaultpurgedays: 30

# Enable hybrid BM25 + semantic search
searchenabled: true

# Global max results per query (0 = use per-query limit)
searchlimit: 25000
# Global default pagination limit for standard retrieval lists and searches (when limit is 0 or omitted)
default_pagination: 100

batchsettings:
    max_batch_size: 100
    harvest_chunk_size: 50
    harvest_inter_batch_sleep_ms: 500
    ingest_inter_batch_sleep_ms: 50
    load_fast_writes_enabled: 0

harvest:
    disable_drift: false
    exclude_dirs:
        - /vendor/
        - /testdata/
        - /mocks
        - /internal/logs
        - /tests
        - /cmd/
```

---

## Data Storage Locations

| Data | Linux | macOS | Windows |
|---|---|---|---|
| **Configuration** | `~/.config/mcp-server-recall/recall.yaml` | `~/Library/Application Support/mcp-server-recall/recall.yaml` | `%APPDATA%\mcp-server-recall\recall.yaml` |
| **Database & Index** | `~/.local/share/mcp-server-recall/.mcp_recall` | `~/Library/Application Support/mcp-server-recall/.mcp_recall` | `%LocalAppData%\mcp-server-recall\.mcp_recall` |
| **JSONL Exports** | `exportdir` in config (defaults to `/tmp`) | `exportdir` in config (defaults to `/private/tmp`) | `exportdir` in config (defaults to `%TEMP%`) |
| **Crash Logs** | `~/.cache/mcp-server-recall/crash.log` | `~/Library/Caches/mcp-server-recall/crash.log` | `%LocalAppData%\mcp-server-recall\crash.log` |

---

## Environment Variables

| Variable | Description |
|---|---|
| `RECALL_ENCRYPTION_KEY` | 64-character hex AES-256 key consumed by `configure` (preferred alias). |
| `MCP_RECALL_ENCRYPTION_KEY` | Same key; also bound by the server loader. |
| `MCP_RECALL_ENCRYPTIONKEY` | Same key without the extra underscore. |
| `MCP_GO_BIN_PATH` | Absolute path override to the Go binary. |
| `MCP_REC_URL` | Explicit service endpoint URL override for Recall. |
| `MCP_SOC_URL` | Explicit service endpoint URL override for Socratic-Thinker. |
| `MCP_ENDPOINT_API_PORT` | Overrides the default HTTP port (`47669`). |
| `MCP_RECALL_SHUTDOWN_TIMEOUT_SECS` | Configures shutdown timeout for goroutines (default: `15`). |
| `HOME` | Standard Unix home directory path reference. |
| `USERPROFILE` | Standard Windows user profile path reference. |
| `GOROOT` | Custom Go installation directory. |

---

*Built with Go. Part of the MagicTools Intelligence Suite.*
