# MCP tools and namespaces

The primary stdio server registers 17 tools. The two Streamable HTTP endpoints
register configurable subsets. Tool arguments are decoded with unknown-field
rejection, so use the exact field names below.

## Endpoint exposure

| Tool | stdio | `/mcp` default | `/mcp/internal` default |
|---|---:|---:|---:|
| `remember` | Yes | No | No |
| `recall` | Yes | No | Yes |
| `get_metrics` | Yes | No | Yes |
| `save_to_recall` | Yes | Yes | Yes |
| `forget` | Yes | No | Yes |
| `reload_cache` | Yes | No | Yes |
| `get_internal_logs` | Yes | Yes | Yes |
| `prune_records` | Yes | No | Yes |
| `export_records` | Yes | No | Yes |
| `import_records` | Yes | No | Yes |
| `ingest_files` | Yes | No | No |
| `search` | Yes | Yes | Yes |
| `list` | Yes | Yes | Yes |
| `get` | Yes | Yes | Yes |
| `delete` | Yes | No | Yes |
| `update_in_recall` | Yes | No | No |

`get_internal_logs` is registered independently of both safe-tool lists, so it
appears on both HTTP endpoints even if omitted from YAML. The default `/mcp`
set is restricted but not read-only because it includes `save_to_recall`.

Restart the server after changing `safetools` or `safetools_internal`.

## Storage namespaces

The record model defines eleven domains:

| Namespace | Intended content |
|---|---|
| `memories` | Unstructured notes and conversation memory. |
| `standards` | Structured standards records. |
| `sessions` | Agent execution state and outcomes. |
| `projects` | Project records. |
| `dialectic_history` | Structured dialogue/history state. |
| `server_status` | Server state records. |
| `modernizer_verdicts` | Modernization verdict state. |
| `modernizer_trust` | Modernization trust/safety state. |
| `madr_state` | Architectural decision state. |
| `ecosystem` | Cross-server ecosystem state. |
| `documents` | Schema-constrained structured documents. |

They are storage domains, not eleven equally supported public APIs. Current
universal-tool dispatch is operation-specific:

| Namespace | Save | Get | Search | List | Delete |
|---|---:|---:|---:|---:|---:|
| `memories` | `remember` | `recall` | Yes | Yes | `forget` |
| `standards` | Yes | Yes | Yes | Yes | Yes |
| `projects` | Yes | Yes | Yes | Yes | Yes |
| `sessions` | Yes | Yes | Yes | Yes | Yes |
| `server_status` | Yes | Yes | Yes | Yes | Yes |
| `dialectic_history` | Yes | Yes | Yes | Yes | Yes |
| `ecosystem` | Yes | Yes | Yes | No | If authorized |
| `modernizer_verdicts` | Yes | Yes | Yes | No | If authorized |
| `modernizer_trust` | Yes | Yes | Yes | No | If authorized |
| `madr_state` | Yes | Yes | No | No | If authorized |
| `documents` | If authorized | If authorized | No | No | If authorized |

Other configured dynamic namespaces can be saved, fetched, deleted, and pruned,
but are not accepted by the current `search` or `list` switch statements.

## Memory tools

### `remember`

Writes one memory or a batch of memories. Single-record `key` and `value` are
required. A batch uses `entries` and cannot be combined with single fields.

```json
{
  "key": "decision:cache-policy",
  "value": "Cache entries expire after 30 days unless pinned.",
  "title": "Cache retention decision",
  "category": "architecture",
  "tags": ["cache", "retention"],
  "dedup_threshold": 0.82
}
```

Batch example:

```json
{
  "entries": [
    {"key": "note:one", "value": "First note", "category": "notes"},
    {"key": "note:two", "value": "Second note", "category": "notes"}
  ]
}
```

Single values are limited to 15 MB. Deduplication applies only to new
memory-domain keys with a non-empty category and positive threshold. It uses
Jaccard similarity over word sets, merging into a same-category record when the
threshold is met.

### `recall`

Accepts exactly one of `key`, `keys`, or positive `count`. With no selector it
returns ten recent memories.

```json
{"keys": ["note:one", "note:two"]}
```

### `forget`

Deletes one key or a batch of keys from `memories` only.

```json
{"keys": ["note:one", "note:two"]}
```

## Structured persistence tools

### `save_to_recall`

Required schema fields are `server_id`, `project_id`, `outcome`, and
`state_data`. `namespace` defaults to `standards`; `memories` is rejected.
`state_data` is stored as a string and is limited to 15 MB.

```json
{
  "namespace": "sessions",
  "key": "planner:session:payments:success:abc123",
  "category": "planner",
  "server_id": "planner",
  "project_id": "payments",
  "outcome": "success",
  "session_id": "abc123",
  "model": "example-model",
  "token_spend": 1200,
  "trace_context": "trace-42",
  "state_data": "{\"summary\":\"Designed the retry flow\"}"
}
```

If `key` is omitted, session, server-status, and ecosystem records get a
server/project/outcome/session matrix key. Other namespaces get a timestamped
key. The `model` and trace fields become tags; `token_spend` is accepted by the
schema but is not persisted in the current record model.

### `get`

Supports an exact `key`, batch `keys` for non-memory namespaces, or an attribute
`query` for non-memory namespaces. `key` and `keys` are mutually exclusive.

```json
{
  "namespace": "projects",
  "query": {
    "tags": ["domain:payments"],
    "tag_match_mode": "all",
    "symbolname": "RetryPolicy"
  }
}
```

Attribute fields are `tags`, `tag_match_mode`, `session_id`, `symbolname`,
`source_path`, and `category`.

### `search`

`query` is required. `namespace` defaults to `standards`.

```json
{
  "namespace": "projects",
  "query": "retry backoff",
  "package": "github.com/example/payments",
  "symbol_type": "func",
  "tags": ["domain:projects"],
  "tag_match_mode": "all",
  "limit": 20
}
```

Available filters depend on namespace:

- memories: `tag`, `limit`;
- sessions/status/history/ecosystem/modernizer: `project_id`, `server_id`,
  `outcome`, `trace_context`, `limit` as accepted by the dispatch path;
- standards/projects: `package`, `symbol_type`, `interface`, `receiver`,
  `domain`, `key_prefix`, `key_suffix`, `tags`, `tag_match_mode`, `limit`;
- standards additionally supports `metadata_only`.

Search is BM25/fuzzy lexical retrieval, not embedding similarity.

### `list`

```json
{
  "namespace": "sessions",
  "project_id": "payments",
  "limit": 50,
  "truncate_content": true
}
```

Accepted pseudo-namespaces are `categories`, `standards_categories`, and
`project_categories`. For memories, `output_format: "aggregations"` also returns
category counts.

### `delete`

```json
{"namespace": "projects", "keys": ["pkg:a:A", "pkg:a:B"]}
```

Standards/projects support selectors including `key`, `keys`, `tags`, category,
package, and `all`. Other structured namespaces support a single `key` or
`all`; batch `keys` are not forwarded for them. `memories` is rejected—use
`forget`.

### `update_in_recall`

The exact fields are `new_key`, `title`, `category`, `tags`, and `replacements`.
The prior README's `replacement_chunks`, `new_category`, and `new_metadata`
names were incorrect.

```json
{
  "namespace": "standards",
  "key": "STD-RETRY-001",
  "new_key": "STD-RETRY-002",
  "title": "Retry standard",
  "category": "architecture",
  "tags": ["retry", "backoff"],
  "replacements": [
    {
      "target": "three attempts",
      "replacement": "five attempts",
      "allow_multiple": false
    }
  ]
}
```

Each replacement is exact string matching. An invalid/empty namespace currently
falls back to `standards`; always provide the intended namespace explicitly.

## Ingest

### `ingest_files`

```json
{"path": "/absolute/path/to/docs", "namespace": "memories"}
```

The default namespace is `memories`. Directory traversal accepts `.md`,
`.yaml`, `.yml`, `.json`, `.txt`, and `.xml`, skipping `.git`, `vendor`, and
`.idea`. Markdown is split by headings, YAML by document boundaries, and the
other accepted types are stored whole. File hashes suppress unchanged re-ingest
and prior records for a changed source path are deleted before replacement.

This full-stdio tool is not in either default HTTP tool list.

## Operations tools

### `get_metrics`

```json
{}
```

Returns host/runtime, storage, namespace, Bleve, write, GC, security-counter,
primitive, and top-query telemetry.

### `get_internal_logs`

```json
{"session_id": "abc123", "max_lines": 100}
```

Both fields are optional. `max_lines` defaults to 25 and is capped at 1000.

### `reload_cache`

```json
{}
```

Rebuilds the Bleve index from BadgerDB. It fails when search is disabled or the
search engine has been detached after exceeding `searchlimit`.

### `prune_records`

```json
{
  "namespace": "sessions",
  "days_old": 30,
  "target_outcome": "",
  "flatten_threshold": 1000,
  "dedup_threshold": 0.7,
  "category": "",
  "report_only": true
}
```

Behavior differs by namespace: memories use dedup/vacuum logic; standards and
projects remove drift/stale records; structured state namespaces prune by age;
`all` runs all configured domains. Use `report_only` before broad maintenance.

### `export_records` and `import_records`

```json
{"filename": "/allowed/export/root/recall.jsonl"}
```

Both paths must be beneath `exportdir` or the OS user-cache root. Export uses
exclusive create and never overwrites. Import upserts; it does not clear the
store first. See [Operations and security](operations-and-security.md).
