# Configuration

`mcp-server-recall configure` writes `recall.yaml`, protects it with mode `0600`
where Unix permissions apply, creates the OS data directory, and opens/closes a
Badger store so a `MANIFEST` normally exists before first service startup.

## Configuration file locations

| OS | File |
|---|---|
| Linux | `${XDG_CONFIG_HOME:-$HOME/.config}/mcp-server-recall/recall.yaml` |
| macOS | `$HOME/Library/Application Support/mcp-server-recall/recall.yaml` |
| Windows | `%APPDATA%\mcp-server-recall\recall.yaml` |

The loader also checks the current working directory for `recall.yaml`, after
the OS-native directory. Keep production configuration in the OS-native path to
avoid working-directory-dependent behavior.

## Real minimal configuration

This is a valid, operationally useful subset. `configure` writes a much larger
commented template, but the runtime does not require every generated key.

```yaml
dbpath: ""
exportdir: ""
encryptionkey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
searchenabled: true
searchlimit: 0
dedupthreshold: 0.8
defaultpurgedays: 30
default_pagination: 100

authorizednamespaces:
  - sessions
  - standards
  - projects
  - server_status
  - dialectic_history
  - documents
  - ecosystem

safetools:
  - save_to_recall
  - search
  - get
  - list

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

batchsettings:
  max_batch_size: 100
  ingest_inter_batch_sleep_ms: 50
  load_fast_writes_enabled: 0
```

Do not reuse the example encryption key. Generate one with
`configure --encrypt-db=true` or supply 64 random hexadecimal characters.

## Active settings

| YAML key | Default | Implemented effect |
|---|---:|---|
| `dbpath` | OS data path | Badger directory; Bleve lives under `search_index` inside it. Relative paths are resolved against the process working directory. Roots and the current working directory are rejected. |
| `exportdir` | OS temporary directory | Allowed root and default destination for JSONL export/import. |
| `encryptionkey` | empty | Hex Badger encryption key. The wizard creates 32 bytes/64 hex characters. |
| `searchenabled` | `true` | Starts Bleve and rebuilds it from Badger at startup. |
| `searchlimit` | `25000` in loader; `0` in generated template | If greater than zero and the record count exceeds it during rebuild, Bleve is disabled for that process. It is not a per-query result limit. |
| `dedupthreshold` | `0.8` | Jaccard word-set threshold for inline deduplication of new, same-category memory records. This is not cosine similarity. |
| `defaultpurgedays` | `30` | Default for `prune_records`/vacuum and telemetry horizons. The CLI `prune` command separately hard-codes 30 when days are omitted. |
| `default_pagination` | `100` | Default search/list page size for most handlers. Session lists use 50. |
| `authorizednamespaces` | built-in list | Allows structured dynamic domains and contributes values to generated MCP schemas. |
| `namespaceschemas` | none | Defines required key names for `save_to_recall` state payloads. Current validation checks for substrings in `state_data`; it does not parse JSON structurally. |
| `safetools` | four tools | Tool subset on `/mcp`; `get_internal_logs` is registered independently. |
| `safetools_internal` | 13 tools | Tool subset on `/mcp/internal`; `get_internal_logs` is registered independently. |
| `batchsettings.max_batch_size` | `100` | Maximum `SaveBatch`/batch-get size captured when the store opens. |
| `batchsettings.load_fast_writes_enabled` | `0` | `1` doubles batch chunks and removes the inter-batch sleep. |

`name` may be present, but the loader always forces it to
`mcp-server-recall`.

## Generated settings that are currently inert

The configuration wizard writes a forward-looking template containing options
that have no runtime accessor or are not applied to the relevant subsystem:

| Generated key/section | Current state |
|---|---|
| `apiport` | Ignored. Use `MCP_ENDPOINT_API_PORT`; default is 47669. |
| `description` | Parsed by Viper but not represented in runtime state. |
| `badgerdb` | Ignored. Badger tuning is hard-coded in `buildBadgerOptions`. |
| `bleveindex` | Ignored. Bleve tuning is hard-coded in `InitStorage`/`Rebuild`. |
| `batchsettings.ingest_inter_batch_sleep_ms` | Loaded but file ingestion currently sleeps a hard-coded 50 ms. |

Changing these values creates no documented runtime effect on current `main`.

## Practical Linux example

```yaml
dbpath: "/srv-data/alex/recall/.mcp_recall"
exportdir: "/home/alex/.cache/mcp-server-recall/backups"
encryptionkey: "replace-with-64-random-hex-characters"
searchenabled: true
searchlimit: 50000
dedupthreshold: 0.82
defaultpurgedays: 45
default_pagination: 100

batchsettings:
  max_batch_size: 100
  load_fast_writes_enabled: 0
```

Create the custom roots before starting the service and restrict access:

```bash
mkdir -p /srv-data/alex/recall/.mcp_recall
mkdir -p "$HOME/.cache/mcp-server-recall/backups"
chmod 700 /srv-data/alex/recall/.mcp_recall
chmod 700 "$HOME/.cache/mcp-server-recall/backups"
```

## Practical macOS example

```yaml
dbpath: "/Users/alex/Library/Application Support/mcp-server-recall/.mcp_recall"
exportdir: "/Users/alex/Library/Caches/mcp-server-recall/backups"
encryptionkey: "replace-with-64-random-hex-characters"
searchenabled: true
searchlimit: 0
dedupthreshold: 0.8
defaultpurgedays: 30
default_pagination: 100
```

Paths containing spaces are ordinary quoted YAML strings; no shell escaping is
needed inside `recall.yaml`.

## Practical Windows example

Use forward slashes in YAML to avoid backslash escaping:

```yaml
dbpath: "C:/Users/alex/AppData/Local/mcp-server-recall/.mcp_recall"
exportdir: "C:/Users/alex/AppData/Local/mcp-server-recall/backups"
encryptionkey: "replace-with-64-random-hex-characters"
searchenabled: true
searchlimit: 0
dedupthreshold: 0.8
defaultpurgedays: 30
default_pagination: 100
```

## Schema-constrained dynamic namespace example

```yaml
authorizednamespaces:
  - sessions
  - standards
  - projects
  - documents

namespaceschemas:
  documents:
    required_keys:
      - DocType
      - SourceAuthority
```

A compatible `save_to_recall` payload is:

```json
{
  "namespace": "documents",
  "server_id": "docs-agent",
  "project_id": "payments",
  "outcome": "indexed",
  "state_data": "{\"DocType\":\"runbook\",\"SourceAuthority\":\"team\",\"body\":\"...\"}"
}
```

The current enforcement only tests whether the strings `DocType` and
`SourceAuthority` occur in `state_data`.

## Environment variables

| Variable | Effect |
|---|---|
| `RECALL_ENCRYPTION_KEY` | Highest-priority key input used by `configure` only. |
| `MCP_RECALL_ENCRYPTION_KEY` | Second-priority `configure` input and a runtime Viper binding. |
| `MCP_RECALL_ENCRYPTIONKEY` | Third-priority alias and runtime Viper binding. |
| `MCP_RECALL_DBPATH` | Overrides `dbpath`. |
| `MCP_RECALL_EXPORTDIR` | Overrides `exportdir`. |
| `MCP_RECALL_SEARCHENABLED` | Overrides `searchenabled`. |
| `MCP_ENDPOINT_API_PORT` | Active HTTP port override. A positive integer is required; invalid values fall back to 47669. |
| `MCP_REC_URL` | Base `/mcp` URL used by CLI clients before they append `/internal`. |
| `MCP_TELEMETRY_UDP_PORTS` | Comma-separated ports/ranges for live dashboard telemetry. |
| `MCP_RECALL_SHUTDOWN_TIMEOUT_SECS` | Positive integer timeout for store background workers; default 15 seconds. |
| `GOMEMLIMIT` | Go memory limit; the binary sets `1024MiB` when absent. |
| `GOMAXPROCS` | Go scheduler limit; the binary sets `2` when absent. |

Viper also maps known YAML keys through the `MCP_RECALL_` prefix. Nested keys
use underscores. Prefer the explicitly tested names above for automation.

## Encryption-key workflows

Generate a new key non-interactively:

```bash
mcp-server-recall configure --encrypt-db=true
```

Supply a managed key on Unix:

```bash
RECALL_ENCRYPTION_KEY="$(openssl rand -hex 32)" \
  mcp-server-recall configure
```

Supply a managed key in PowerShell:

```powershell
$bytes = New-Object byte[] 32
$rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
$rng.GetBytes($bytes)
$rng.Dispose()
$env:RECALL_ENCRYPTION_KEY = -join ($bytes | ForEach-Object { $_.ToString('x2') })
mcp-server-recall.exe configure
Remove-Item Env:RECALL_ENCRYPTION_KEY
```

`configure --force` resets the file to the generated defaults. On an attached
terminal it asks before overwriting; in non-interactive use, `--force` can
overwrite without that prompt. Back up both YAML and data before using it.

Changing or removing an encryption key does not re-encrypt the existing store.
Export with the old key, create a new store, then import when rotating keys.

## Reload behavior

Viper watches `recall.yaml` and refreshes its in-memory state. Some consumers
read configuration per request, while store/search construction settings are
captured at startup. Restart `serve` after configuration edits for deterministic
application of `dbpath`, encryption, search, batch size, HTTP port, and tool
exposure.
