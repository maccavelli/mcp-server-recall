# Repository assessment

This assessment records the evidence used to rewrite the documentation. It
describes the `main` source tree **as audited on 2026-08-29 at commit
`90c4bef`**, not an aspirational product design.

> **Partly superseded by
> [0005-MADR](../0005-MADR-remove-harvest-and-go-toolchain-dependency.md).**
> The harvest subsystem, both `harvest` CLI commands, the `harvest` MCP tool,
> every `harvest.*` configuration key, and the entire dependency on an external
> Go toolchain have since been removed. Findings below that concern harvesting
> are retained as an accurate record of what was audited, annotated inline.
> They no longer describe the current tree.

## Audit scope and baseline

Audit date: 2026-08-29  
Audited commit: `90c4bef` (`main`)  
Local toolchain: `go version go1.26.5 darwin/arm64`

Commands executed:

```bash
go build -o /tmp/mcp-server-recall-doc-audit ./cmd/mcp-server-recall
/tmp/mcp-server-recall-doc-audit --help
/tmp/mcp-server-recall-doc-audit configure --help
/tmp/mcp-server-recall-doc-audit harvest --help
/tmp/mcp-server-recall-doc-audit export --help
/tmp/mcp-server-recall-doc-audit import --help
/tmp/mcp-server-recall-doc-audit prune --help
/tmp/mcp-server-recall-doc-audit purge --help
/tmp/mcp-server-recall-doc-audit dash --help
go test ./...
sh scripts/install_test.sh
```

All Go packages passed, and the installer suite reported 48 passed and 0
failed. The audit also read CLI, configuration, storage, search, harvest, MCP
registration/handlers, telemetry/dashboard, installer, tests, and CI source.

External facts were checked against:

- the [latest GitHub release](https://github.com/maccavelli/mcp-server-recall/releases/latest);
- the [Go downloads page](https://go.dev/dl/);
- [`os.UserConfigDir` and `os.UserCacheDir`](https://pkg.go.dev/os#UserConfigDir);
- the [MCP transport specification](https://modelcontextprotocol.io/specification/draft/basic/transports);
- the [VS Code MCP configuration reference](https://code.visualstudio.com/docs/agents/reference/mcp-configuration);
- the [BadgerDB project](https://github.com/dgraph-io/badger).

## Overall state

The repository is an actively tested, single-binary Go MCP service with broad
functionality and a strong amount of unit/integration coverage. Its durable
storage, namespace isolation checks, full-text index rebuild, CLI-to-local-MCP
flow, OS-native paths, installers, and terminal telemetry are implemented in
code and tests.

The largest gap was documentation accuracy. The previous README blended
implemented features, stale interfaces, historical terminology, and
forward-looking configuration. Some runtime descriptions and dashboard labels
remain misleading in source and are now called out explicitly.

## Functional assessment

| Area | State | Evidence and qualification |
|---|---|---|
| Badger persistence | Implemented and well tested | `internal/memory/badger.go` and extensive memory tests cover CRUD, batch operations, locking, encryption startup, portability, vacuum, index repair, and close behavior. |
| Record compression | Implemented | `internal/memory/record.go` Zstd-compresses serialized records and migrates legacy formats. |
| Search | Implemented, mislabeled | `internal/search` implements Bleve BM25 content search plus `sahilm/fuzzy` key matching. No embedding model or vector index exists. |
| Namespace isolation | Implemented but uneven at tool layer | Eleven record domains exist. Universal save/get/search/list/delete switch statements support different subsets. |
| Go harvesting | **Removed by 0005-MADR** | At audit time `internal/harvest` used Go AST, types, `go/packages`, `go doc`, remote module resolution, examples, interface checks, and checksums. The package no longer exists. |
| File ingestion | Implemented with narrower formats than template | `ProcessPath` accepts Markdown, YAML, JSON, text, and XML; generated extension configuration is ignored. |
| MCP stdio | Implemented | Primary server registers 17 tools and uses the official Go MCP SDK IO transport. |
| Streamable HTTP | Implemented locally | `/mcp` and `/mcp/internal` bind to 127.0.0.1. Tool subsets are configurable; no authentication or Origin validation exists. |
| CLI | Implemented | Cobra exposes configure/init, serve, dash, export, import, prune, purge, version/help, and generated completion. ~~two harvest commands~~ removed by 0005-MADR. |
| Dashboard | Implemented with caveats | Eight pages combine UDP and persisted telemetry. Several labels/values are static, heuristic, stale, or incomplete. |
| Installers | Implemented and released in v1.1.0 | POSIX and PowerShell scripts verify SHA-256, install per-user, and configure encryption. |
| Platform CI | Strong | CI runs formatting, tidy, vet, race/cgo-free tests, lint, installer tests, cross-builds, native Linux arm64/Windows amd64 jobs, and tag-release checks. |

## CLI findings

- `export`, `import`, and `prune` are HTTP clients, not offline datastore
  commands. They require a running `serve` instance. (`harvest` was also one;
  removed by 0005-MADR.)
- Each connects to `ResolveRecallURL() + "/internal"`, waiting up to ten seconds.
- `purge` is the exception: it edits the datastore directory directly and
  should be used only after stopping `serve`.
- Root invocation without a subcommand runs `serve`.
- The CLI prune syntax is `prune <namespace> [days]`; the prior README showed
  `prune [days]` and attributed its default to YAML, but the CLI hard-codes 30.
- Import is an upsert, not a database wipe. The registered tool prose and CLI
  long description overstate replacement semantics.
- `configure --encrypt-db=true` securely generates a 32-byte key and
  materializes the store. This setup flow is included in v1.1.0.

## Configuration findings

The generated template is not a faithful runtime schema.

Implemented and used fields include paths, encryption, search enable/limit,
Jaccard dedup threshold, retention/pagination, authorized namespaces, simple
schema rules, safe tool lists, and — at audit time — harvest chunk tuning and
exclude dirs, both removed by 0005-MADR.

Generated but currently unused fields include:

- `apiport` (the live setting is `MCP_ENDPOINT_API_PORT`);
- the entire `badgerdb` section;
- the entire `bleveindex` section;
- `harvest.categories`, `harvest.excludes`, and `harvest.extensions` (the whole
  `harvest:` block has since been removed by 0005-MADR);
- `description`;
- the loaded `ingest_inter_batch_sleep_ms` value.

Additional discrepancies:

- the loader default for `searchlimit` is 25000, while a generated file sets 0;
- the loader's default authorized list and generated file's list differ;
- the generated template includes `documents` and `ecosystem` but omits
  modernizer/MADR domains that exist in code;
- `namespaceschemas` validation checks substring presence in `state_data`, not
  parsed JSON keys;
- Viper hot-reloads state, but several subsystems capture values at startup, so
  restart is the reliable operational rule.

## Search findings

The implemented algorithm is explicit in `internal/search/engine.go`:

1. Bleve queries analyzed title, symbol name, content, category, tags, source
   path, and source hash using a technical analyzer and BM25 scoring.
2. `sahilm/fuzzy` performs character-subsequence matching over known keys.
3. Results are merged, deduplicated, normalized, and sorted.

There is no embedding dependency, model loading, vector field, approximate
nearest-neighbor structure, or cosine retrieval. `dedupthreshold` drives
Jaccard word-set comparison for memory writes, not cosine similarity.

The documentation now uses “full-text and fuzzy search.” Historical source
strings such as “Semantic Search Engine,” “vector recall,” and “embedding
matrix” remain implementation-label debt.

## Namespace findings

The prior README claimed nine namespaces. `memory.AllDomains` contains eleven:
the prior nine plus `ecosystem` and `documents`.

Tool support is asymmetric. For example, `documents` can be authorized for
save/get/delete but is rejected by the universal search/list switches;
`madr_state` can be saved and fetched but not searched or listed. The new MCP
guide includes an operation matrix rather than promising uniform support.

Dynamic namespaces are cached in `.recall-dynamic-namespaces.json` after new
domain events and merged into authorized schema values. This does not add a new
case to hard-coded search/list dispatch.

## Security findings

Positive controls:

- loopback-only HTTP bind plus a second remote-address check;
- Badger optional encryption key and data-key rotation;
- restrictive config/export/telemetry file modes on Unix;
- import/export path containment checks;
- unknown MCP argument rejection;
- namespace checks on key retrieval/deletion;
- singleton and Badger directory locks;
- atomic configuration, telemetry, and search-rebuild publication patterns;
- purge path and `MANIFEST` validation.

Important limitations:

- YAML holds the key in plaintext;
- Bleve and JSONL exports have no application encryption;
- HTTP has no authentication and does not validate Origin;
- `/mcp` is not read-only because `save_to_recall` is enabled by default;
- diagnostic logs are exposed on both HTTP endpoints independently of safe
  lists;
- path containment is lexical and does not resolve symlinks;
- dashboard cryptography status rows are unconditional strings.

## Dashboard findings

The old README listed tabs that do not match the UI. Current navigation is:

1. Summary
2. Memory Consolidation & GC
3. Semantic Search Engine
4. Taxonomy & AST Pipeline
5. RPC & Gateway Analytics
6. Network Topology
7. Security & Cryptography
8. Config & Environment
9. Quit

There is no Event Log tab; Summary includes the log tail. The fallback reader
uses `telemetry.ring`, although the UI string calls it BuntDB. Namespace tables
show nine of eleven domains. AST derivations are estimated as two times project
records. Security encryption rows and log-level display are static.

The underlying telemetry is nevertheless substantive: runtime/CPU/memory,
Badger sizes and hit counters, Bleve counts/drift, write/GC operations, category
distribution, query statistics, connected HTTP sessions, and log events.

## Release and platform findings

The current CI builds:

- Linux amd64;
- Linux arm64;
- macOS arm64;
- Windows amd64.

At the initial audit baseline, v1.0.2 checksums listed only Linux amd64, macOS
arm64, and Windows amd64. It did not include the installer scripts or Linux
arm64 asset added in later commits. The v1.1.0 release closes that publication
gap and makes the documented bootstrap commands the primary install path.

The platform guide now provides verified manual-release downloads and separate
current-source build instructions. It avoids claiming Intel macOS, Windows
arm64, or 32-bit support.

## Repository health and remaining gaps

Strengths:

- clean working tree at audit start;
- cohesive Go package layout;
- broad test suite with platform-specific cases;
- race/cgo-free/native CI intent;
- explicit architectural records and approved implementation plans;
- safe installer/checksum design on `main`;
- careful datastore shutdown and rebuild behavior.

Remaining engineering/documentation debt:

- publish a new release so documented `main` functionality and release assets
  converge;
- replace or remove semantic/vector terminology throughout CLI/tool/dashboard
  strings;
- make generated YAML reflect only active settings, or wire all generated
  settings into runtime behavior;
- make namespace operation support consistent or schema-driven;
- make the dashboard's encryption/log/AST values measured or label them as
  static estimates in the UI;
- keep the source fallback version synchronized with future release tags;
- ~~fix harvest error text that recommends unused `RECALL_GO_BIN`~~ — resolved
  by 0005-MADR: the code carrying that text was deleted;
- decide whether `/mcp` should truly be read-only and whether logs belong there;
- add explicit Origin/auth controls before any non-stdio deployment;
- add a root license file if public reuse is intended—the audited repository
  contains no `LICENSE` file.

## README corrections made by this documentation library

The rewrite removes or corrects these prior claims:

- “hybrid BM25 + semantic/vector recall” → BM25 plus fuzzy key matching;
- nine namespaces → eleven storage domains with an operation support matrix;
- installer availability → the v1.0.2 gap is recorded and v1.1.0 is the first
  release using the bootstrap path;
- VS Code `mcpServers` syntax → current official `servers` syntax;
- YAML `apiport` controls HTTP → environment variable controls HTTP;
- generated Badger/Bleve tuning is configurable → sections are currently inert;
- cosine deduplication → Jaccard word-set deduplication;
- import wipes/replaces the store → import upserts;
- prune syntax/default → namespace is required and CLI default is hard-coded;
- dashboard Event Log tab → event tail is on Summary;
- dashboard cryptography status → static display, not attestation;
- all data encrypted at rest → explicit Badger/Bleve/config/export boundaries;
- all namespaces uniformly searchable/listable → per-operation support is shown;
- MCP update field names → exact `replacements`, `category`, and `tags` schema.
