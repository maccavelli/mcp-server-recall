# Operations and security

This server is designed for local, user-scoped operation. Its strongest
boundaries are filesystem permissions, process isolation, loopback binding,
namespace checks, and import/export path validation. It is not a remotely
authenticated service.

## Storage layout

| Item | Location |
|---|---|
| Badger source of truth | `<dbpath>` |
| Bleve search index | `<dbpath>/search_index` |
| Persisted dashboard snapshot/log ring | `<dbpath>/telemetry.ring` |
| Dynamic namespace cache | `<config-dir>/.recall-dynamic-namespaces.json` |
| Configuration and encryption key | `<config-dir>/recall.yaml` |
| Crash lifecycle log | OS application cache directory, `crash.log` |
| Singleton lock | OS temporary directory, `mcp-server-recall.lock` |

Default paths are listed in [Platform installation](platform-installation.md).

Badger records are serialized JSON compressed with Zstd. Secondary index keys
and domain/category/time indices share the same Badger store. Bleve is rebuilt
from Badger on each search-enabled startup, making Badger the source of truth.

## Encryption scope

When `encryptionkey` is set, the server passes the decoded key to Badger and
configures a seven-day data-key rotation interval. The wizard generates a
32-byte/64-hex-character key. Badger also accepts 16- or 24-byte keys at its
lower-level constructor, but `configure` intentionally requires 32 bytes.

Important boundaries:

- `recall.yaml` contains the encryption key as plaintext. On Unix it is written
  with mode `0600`; protect the account and backups of this file.
- Badger's on-disk encryption does not configure encryption for the separate
  Bleve directory.
- Bleve mappings set fields to `Store=false`, so original field bodies are not
  retrievable from the index alone, but indexed terms and index structures are
  still sensitive and are not encrypted by this application.
- `telemetry.ring` is written with mode `0600` and can include paths, query
  strings, and log messages.
- JSONL exports are plaintext complete record backups, including content and
  metadata, but not the encryption key.

The dashboard's Curve25519/AES-GCM status rows are static strings and cannot be
used to verify any of these properties.

## Key management and rotation

Keep the exact key with the datastore. Opening encrypted Badger data with an
empty or different key fails; changing YAML does not migrate data.

A safe rotation sequence is:

1. Start the server with the old key.
2. Export a JSONL backup into an allowed, protected directory.
3. Stop `serve`.
4. Move the old datastore to a protected backup location or use `purge` only
   after verifying the export.
5. Configure a new key and materialize a new store.
6. Start `serve` with the new store.
7. Import the JSONL backup.
8. Verify counts and search, then securely manage the plaintext export.

Do not run `configure --force`, `--encrypt-db=false`, or key-changing automation
against an existing encrypted store without this recovery path.

## Local HTTP boundary

The HTTP listener uses `127.0.0.1:<port>` and middleware rejects remote
addresses other than `127.0.0.1`, `::1`, or the literal `localhost`. This is a
useful local boundary, but:

- there is no authentication or authorization identity beyond endpoint tool
  lists;
- Origin headers are not validated;
- any process running as a local user that can reach the port may call exposed
  tools;
- `/mcp` includes a write tool by default;
- `get_internal_logs` is exposed on both endpoints regardless of safe-tool YAML.

The [MCP transport specification](https://modelcontextprotocol.io/specification/draft/basic/transports)
recommends localhost binding, Origin validation, and authentication for
Streamable HTTP. This implementation provides the first control only.

Never publish or forward the port. Use stdio when a client can launch the server
directly.

## Import/export sandbox

Both CLI commands resolve the supplied path to absolute. The server then allows
it only when it is equal to or under either:

- configured `exportdir` (the OS temporary directory when empty); or
- the OS user-cache root returned by `os.UserCacheDir`.

The cache allowance is the OS root (`~/.cache`, `~/Library/Caches`, or
`%LOCALAPPDATA%`), not only the `mcp-server-recall` subdirectory. Use a dedicated
application backup directory anyway.

The lexical check cleans paths and uses `filepath.Rel`; it does not resolve
symlinks before deciding containment. Do not place attacker-controlled symlinks
inside an allowed root.

## Backup and restore

### Configure a dedicated backup directory

Linux:

```yaml
exportdir: "/home/alex/.cache/mcp-server-recall/backups"
```

macOS:

```yaml
exportdir: "/Users/alex/Library/Caches/mcp-server-recall/backups"
```

Windows:

```yaml
exportdir: "C:/Users/alex/AppData/Local/mcp-server-recall/backups"
```

Create the directory with user-only access where the OS supports it, restart
the server, and export to a new filename:

```bash
mcp-server-recall export /configured/backup/root/recall-2026-08-29.jsonl
```

Export uses `O_EXCL` and returns an error if the path already exists. This
prevents accidental overwrite but means scheduled jobs need unique names.

### JSONL content

Each line contains:

```json
{
  "key": "record-key",
  "title": "Record title",
  "content": "Plaintext record body",
  "category": "architecture",
  "domain": "memories",
  "tags": ["example"],
  "created_at": "2026-08-29T10:00:00Z",
  "updated_at": "2026-08-29T10:00:00Z"
}
```

Internal `_idx:` entries, the Bleve index, telemetry, and encryption key are
not exported.

### Restore semantics

```bash
mcp-server-recall import /configured/backup/root/recall-2026-08-29.jsonl
```

Import does not clear the database. It batches up to 100 lines and upserts by
key. New records preserve exported timestamps. Existing keys keep the target
store's original creation time and receive imported values/updated time.
Malformed lines can produce partial success.

For a replacement-style restore, stop the server, preserve or purge the old
store, create an empty store, restart, and then import.

## Pruning and retention

The CLI form is destructive:

```bash
mcp-server-recall prune sessions 30
```

The MCP `prune_records` tool supports `report_only` and should be used to audit
broad memory, project, standards, or `all` maintenance before mutation.

The YAML `defaultpurgedays` feeds the MCP tool when `days_old` is absent and
dashboard horizon metrics. The CLI defaults independently to 30 days.

Memory records are exempt from simple age-based session pruning, but memory
vacuum can deduplicate or consolidate them. Test report-only output on a backup
before enabling automated maintenance.

## Purging a datastore

`purge` removes the configured directory recursively. Its safeguards require a
Badger `MANIFEST`, reject roots/current-working-directory relationships, and
ask for confirmation unless `--force` is used.

Operational checklist:

1. Stop `serve` to release Badger and Bleve file locks.
2. Resolve `dbpath` from the same environment used by the server.
3. Export and verify a backup.
4. Run `mcp-server-recall purge` interactively.
5. Run `configure --encrypt-db=true` to create a fresh store.

## Search recovery

Bleve is rebuilt from Badger during every search-enabled startup. It can also be
rebuilt online:

```json
{}
```

Call that payload with `reload_cache`. The rebuild uses a staging directory and
atomic directory swap with a backup/rollback attempt. If the configured
`searchlimit` is positive and lower than total records, the server detaches
Bleve for that process instead.

## Process and file safety

- `serve` uses a singleton lock to prevent two processes opening the same
  service configuration concurrently.
- Badger also has its own datastore directory lock.
- `configure` writes YAML atomically and uses restrictive directory/file modes.
- Export uses exclusive creation and mode `0600`.
- Telemetry publication writes a temporary file and renames it atomically.
- The binary writes a basic `MAIN STARTING`/`MAIN EXITED` crash lifecycle log;
  it is not a complete crash dump.

## Threat-model summary

Suitable assumptions:

- trusted local user account;
- trusted MCP client configuration;
- no untrusted local processes sharing that account;
- loopback HTTP is not exposed;
- config, exports, and cache directories are protected;
- external Go modules and harvested inputs are treated as untrusted data.

Do not treat the current design as multi-user isolation, a remotely exposed
database, a secrets vault, or end-to-end encrypted search.
