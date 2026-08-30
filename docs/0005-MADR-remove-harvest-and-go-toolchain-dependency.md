---
status: "accepted"
date: 2026-08-29
decision-makers: maccavelli
consulted: maccavelli
informed: maccavelli
---

# 0005-MADR: Remove the harvest subsystem and every dependency on an external Go toolchain

## Context and Problem Statement

`mcp-server-recall` ships as a static, CGO-free binary (`Makefile:17`,
`CGO_ENABLED=0 ... -tags netgo -extldflags '-static'`) and is installed by a
`curl | sh` bootstrap that explicitly does not require Go
([0003](0003-MADR-curl-bootstrap-installer.md),
[0004](0004-MADR-installer-runs-configure.md)). One subsystem breaks that
promise: **harvest**.

Harvest shells out to a real `go` binary at runtime. `internal/harvest/gobin.go`
searches for one in four places — `MCP_GO_BIN_PATH`, `$GOROOT/bin/go`, a
hard-coded scan of `~/sdk/go1.26.5`, `~/sdk/go1.26.1`, `~/sdk/go1.25.0`,
`~/.local/go/bin`, `~/go/bin`, `/usr/local/go/bin/go`, `/usr/lib/go/bin/go`
plus a `~/sdk/go*` glob, and finally `exec.LookPath("go")`
(`internal/harvest/gobin.go:27-89`). Having found one it executes, at MCP
request time:

| Command | Site |
|---|---|
| `go doc -all <pkg>` | `internal/harvest/engine.go:225` |
| `go mod init temp` | `internal/harvest/engine.go:83` |
| `go get <pkg>@latest` | `internal/harvest/engine.go:93` |
| `go list -f {{.Dir}} <pkg>` | `internal/harvest/resolver.go:48`, `:105` |
| `go mod init tmp` | `internal/harvest/resolver.go:77` |
| `go get <pkg>/...@latest` | `internal/harvest/resolver.go:92` |

`goEnv()` additionally mutates the **live server process's** `PATH` via
`os.Setenv` so that `go/packages`' own `exec.LookPath` succeeds
(`internal/harvest/gobin.go:134-157`, the two `os.Setenv` calls at `:144` and
`:150`). `internal/harvest/engine.go` imports
`go/ast`, `go/printer`, `go/token`, `go/types`, `golang.org/x/tools/go/packages`
and `golang.org/x/tools/go/ast/inspector`, which is the sole reason
`golang.org/x/tools v0.45.0` is a direct requirement in `go.mod:19` and pulls
**19 `golang.org/x/tools` packages** into the shipped binary's dependency graph
(`go list -deps ./... | grep -c golang.org/x/tools` → 19), plus
`golang.org/x/mod` as an indirect.

This is the only such dependency in the repository. A grep for
`exec.Command`, `exec.LookPath`, `go/packages`, `go/ast`, `go/types`,
`GOROOT`, `GOPATH`, `MCP_GO_BIN_PATH` across all non-test `.go` files returns
hits in `internal/harvest/` **and nowhere else**. Removing that one package
removes the toolchain requirement from the product entirely.

The cost is not only the dependency. Harvest is also the subsystem that behaves
least like a session recorder and document store:

* It performs **outbound network I/O the operator did not request**:
  `go get ...@latest` downloads arbitrary remote modules into the user's real
  module cache (`internal/harvest/resolver.go:92`), and when that fails the
  resolver silently cascades to `https://` + the input string and *scrapes a web
  page* into the datastore — two cascade sites, `internal/harvest/resolver.go:99`
  (after a failed `go get`) and `:120` (after a failed `go list`), feeding
  `internal/harvest/scraper.go:17`.
* It sleeps in the request path — `time.Sleep(50 * time.Millisecond)` per
  package (`internal/harvest/engine.go:186`) and up to
  `harvest_inter_batch_sleep_ms: 500` per write chunk
  (`internal/server/handlers_harvest.go:255-258`).
* The registered tool description actively lies about what it does. It reads
  "batch-ingesting raw file system directories into the recall backend"
  (`internal/server/registration.go:78`) while the handler is a Go AST/types
  extractor. `docs/guides/cli-reference.md:135-136` already carries a
  correction: "Despite the general 'codebase' wording, this command performs Go
  AST/type harvesting. It is not a language-independent source indexer."
* Its own error strings recommend an environment variable the code never reads:
  `internal/harvest/engine.go:223` and `internal/harvest/resolver.go:45` say
  "set `RECALL_GO_BIN`", but the resolver only honours `MCP_GO_BIN_PATH`
  (`internal/harvest/gobin.go:30`). `internal/harvest/engine_deps_test.go:11-14`
  sets `RECALL_GO_BIN`, which is inert. This defect is already listed as
  outstanding debt in `docs/guides/repository-assessment.md:235`.

Meanwhile the reach of the feature is small. The `harvest` MCP tool is
registered **only** when a `safeMap` is supplied and contains it
(`internal/server/registration.go:77`); `registerTools()` passes `nil`
(`registration.go:90`), so the primary stdio server never exposes it. It reaches
clients solely through `safeToolsInternal` on the localhost-only
`/mcp/internal` endpoint (`internal/config/config.go:165`,
`cmd/mcp-server-recall/config_template.go:42`), whose only in-tree caller is the
CLI (`cmd/mcp-server-recall/harvest.go:78-82`).

Nothing else depends on harvest to populate the `standards` and `projects`
namespaces: `save_to_recall` writes both (`internal/server/server.go:196`,
`:232`), `update_in_recall` edits both, and `ingest_files` walks the filesystem
through `MemoryStore.ProcessPath` (`internal/memory/ingest.go:444`) with no
toolchain involvement at all.

**Problem:** the server's intended purpose is a session recorder and a
document/state/status store. Harvest makes a full Go toolchain a de facto
runtime requirement for a binary distributed precisely so users would not need
one, adds 19 third-party packages and ~1,275 lines of code to that binary, and
performs unrequested network fetches from inside an MCP request. It should be
removed.

## Decision Drivers

* The shipped artefact must be self-contained. Installing via `curl | sh` must
  never imply "and also install Go."
* Reduce the binary's third-party surface and the audit surface that comes with
  `exec`ing an external program discovered by filesystem scan.
* No MCP tool should make unrequested outbound network requests or write to the
  user's Go module cache.
* **Databases created by v1.0.0–v1.1.1 already contain harvested records.**
  Removal must not strand them: they must remain listable, searchable, and
  deletable.
* Removal must be complete. A half-removed feature that leaves configuration
  keys, telemetry fields, and documentation behind is worse than the feature.

## Considered Options

* **Option 1 — Keep harvest, fix only the defects.** Correct the `RECALL_GO_BIN`
  error text, fix the tool description, gate the web-scraper cascade.
* **Option 2 — Keep harvest but make the toolchain optional at build time.**
  Move `internal/harvest` behind a `//go:build harvest` tag and ship two
  binaries.
* **Option 3 — Remove the harvest write path entirely; retain read/delete
  support for records already stored.** (Recommended.)
* **Option 4 — Remove harvest and every trace of harvested data, including the
  `HarvestedCategories` storage constants and the `standards`/`projects`
  overview/search paths built on them.**

## Decision Outcome

Chosen option: **Option 4 — remove harvest, every Go-toolchain dependency, and
the entire harvested-category storage layer. No migration, no data retention.**

> **Decision history.** This MADR first proposed Option 3 (keep the storage-layer
> read/delete paths so records written by v1.0.0-v1.1.1 stayed reachable). On
> 2026-08-29 the operator selected **Option 4** instead: no migration and no
> retention. The sections below are written to Option 4. Option 3 remains
> recorded above as a considered option, and the trade-off it was protecting is
> stated plainly under Consequences.

Option 1 keeps the runtime toolchain requirement, which is the actual problem.
Option 2 keeps the code, the `golang.org/x/tools` requirement in `go.mod`, and
the test matrix, while adding a second release artefact and a support question
("which binary do I have?") for a feature the operator has decided is
unnecessary. Option 3 was rejected by the operator as unnecessary caretaking of
data they do not need.

### What is deleted

**1. The `internal/harvest` package in full** — 9 files, 1,275 lines
(943 excluding tests):

| File | Lines | Contents |
|---|---|---|
| `engine.go` | 582 | AST/types extractor, `go doc -all`, `go mod init`, `go get` |
| `gobin.go` | 157 | `MCP_GO_BIN_PATH`/`GOROOT`/SDK-scan resolution, `PATH` mutation |
| `resolver.go` | 126 | `go list`, remote module fetch, `https://` cascade |
| `scraper.go` | 78 | `ScrapeWebDocument` outbound HTTP fetch |
| `engine_test.go`, `engine_deps_test.go`, `gobin_test.go`, `resolver_test.go`, `scraper_test.go` | 332 | tests |

**2. The CLI commands** — `cmd/mcp-server-recall/harvest.go` (94 lines) and
`harvest_test.go` (20 lines). This deletes `harvestCmd`, `harvestStandardsCmd`,
`harvestProjectsCmd`, and `runHarvestViaMCP`, and with them the
`RootCmd.AddCommand(harvestCmd)` registration (`harvest.go:92`).
`mcplib.NewRecallClient` / `WaitForRecallServer` remain in use by `export.go`,
`import.go`, and `prune.go`, so `mcplib` stays a dependency.

**3. The MCP handler layer** — `internal/server/handlers_harvest.go` (266 lines)
and `handlers_harvest_test.go` (154 lines), removing `handleHarvestStandards`,
`handleHarvestProjects`, `handleHarvest`, `hasDrifted`, `ingestHarvestResult`,
`buildSymbolEntry`, `extractModuleName`, `detectDomainTags`, and
`writeHarvestBatch`.

**4. The tool binding** —
`handleUniversalHarvest` and `UniversalHarvestInput`
(`internal/server/handlers_consolidated.go:78-84`, `:229-238`); the registration
block at `internal/server/registration.go:77-79`; `"harvest"` in the
`ReloadTools` name list (`registration.go:101`); `HarvestStandardsInput` and
`HarvestProjectsInput` (`handlers_structs.go:18-30`); the constants
`harvestAuth`, `harvestDatabase`, `harvestTest`
(`internal/server/constants.go:33-35`), whose only consumer is
`detectDomainTags`.

`HarvestInput` (`handlers_structs.go:140-145`) is deleted as part of this work
as well. Grep confirms it is **declared and never referenced anywhere**,
including tests — it is already dead code.

**5. The configuration surface:**

* `HarvestConfig` and the `State.Harvest` field (`internal/config/config.go:43-46`,
  `:63`).
* Accessors `ExcludeDirs()` (`config.go:340-346`) and `HarvestDisableDrift()`
  (`config.go:383-388`), and the `disable_drift_applied_status` reload log line
  (`config.go:212`).
* Defaults `harvest.exclude_dirs` and `harvest.disable_drift`
  (`config.go:132-133`).
* `BatchConfig.HarvestChunkSize` and `BatchConfig.HarvestInterBatchSleepMs`
  (`config.go:37-38`) with their defaults (`config.go:126-127`). Both are read
  only by `writeHarvestBatch`; `MaxBatchSize`, `IngestInterBatchSleepMs`, and
  `LoadFastWritesEnabled` are used elsewhere and stay.
* `"harvest"` in the default `safeToolsInternal` list (`config.go:165`).
* In `cmd/mcp-server-recall/config_template.go`: `- harvest` under
  `safetools_internal` (line 42), the two `batchsettings` keys (lines 127-128),
  and the whole `harvest:` block (lines 149-183). Note that block's
  `categories`, `excludes`, and `extensions` keys have **never** been mapped to
  any struct field and are already documented as ignored
  (`docs/guides/configuration.md:123-125`,
  `docs/guides/repository-assessment.md:99`); deleting the block removes three
  pre-existing lies from the generated file.

**6. Telemetry and dashboard:**

* The `ASTStats` type (`internal/telemetry/snapshot.go:78-81`), the `AST`
  field it backs on the snapshot struct (`snapshot.go:110`), and its population
  (`snapshot.go:262-265`). Its `ParsedFiles` value is not measured — it is
  `metrics.Namespaces[DomainProjects] * 2`, commented "Heuristic mapping" — and
  its other two fields are the harvest config values being deleted.
* The **AST panel only** of the Taxonomy dashboard page. This needs care:
  `renderTaxonomyAST` (`cmd/mcp-server-recall/dashboard_views.go:384-453`)
  renders **three** panels, and only the first is harvest-related:

  | Panel | Lines | Action |
  |---|---|---|
  | "AST Ingestion Pipeline" (`snapshot["ast"]`) | 388-399 | **delete** |
  | "Taxonomy & Tag Distribution" (`snapshot["taxonomy"]`) | 401-426 | **keep** |
  | "Category Distribution (Top 10)" (`snapshot["categories"]`) | 429-450 | **keep** |

  So the function is *trimmed and renamed* (`renderTaxonomyAST` →
  `renderTaxonomy`), not deleted. The `lipgloss.JoinHorizontal(lipgloss.Top,
  astBox, taxBox)` at `:428` becomes `taxBox` alone.

  Correspondingly the tab is **renamed, not removed**: `tabTaxonomyAST` →
  `tabTaxonomy` (`cmd/mcp-server-recall/dashboard.go:59`), its label
  "Taxonomy & AST Pipeline" → "Taxonomy & Categories" (`:71`), and its dispatch
  (`:324-325`). Because `tabTaxonomyAST` is an `iota` constant in a block whose
  ordering is mirrored by the parallel `navItems` slice, **deleting** it would
  silently renumber `tabRPCAnalytics`, `tabNetwork`, `tabSecurity`, `tabConfig`,
  and `tabQuit` and desynchronise the nav labels. Renaming keeps every index
  stable and is the only safe move here.

**7. The module requirement** — `golang.org/x/tools v0.45.0` from the direct
require block (`go.mod:19`), and `golang.org/x/mod` from the indirect block,
followed by `go mod tidy`. Nothing outside `internal/harvest/engine.go:22-23`
imports `golang.org/x/tools`.

### What is also deleted — the harvested-category storage layer

Option 4 extends the deletion into `internal/memory`. Investigation on
2026-08-29 found this reaches considerably further than the original Option 3
sketch implied, so the full inventory is recorded here.

`HarvestedCategories` = `{HarvestedCode, PackageDoc, SysDrift}`
(`internal/memory/badger.go:38-41`) plus the constants `catHarvestedCode` and
`catSysDrift` (`internal/memory/constants.go:5-6`) are deleted, together with
every consumer:

| Site | What it does today | Action |
|---|---|---|
| `badger.go:898` (`Save`) | rejects harvested categories written to `memories` | delete the guard |
| `badger.go:1838-1912` (`VacuumStandards`) | orphan detection over `SysDrift` keys | **rewrite** (see below) |
| `badger.go:1964` (`searchByTag`) | filters harvested records out of tag search | delete the filter |
| `badger.go:2789` (`SaveBatch`) | infers `Domain = standards` from category | default to `memories` |
| `badger.go:2880` (`syncBatchToSearchIndex`) | same inference for the synthetic `domain:` tag | default to `memories` |
| `badger.go:3047` (`ListCategories`) | excludes harvested categories | delete the filter |
| `badger.go:3067-3220` | `StandardsPackageOverview`, `ListStandardsOverview`, `scanHarvestedCodeIndex`, `scanPackageDocIndex`, `scanSysDriftIndex` | **rewrite** (see below) |
| `badger.go:3230` (`recordMatchesDomainSearch`) | skips `SysDrift` records in domain search | delete the skip |
| `badger.go:3336-3400` (`ListDomainOverview`) | same package-grouping for `projects` | **rewrite** (see below) |
| `badger.go:3801`, `:3864`, `:3916` (`DeleteStandards`/`DeleteProjects`) | uses harvested categories as an *inclusion* filter | invert to accept any category in the domain |
| `record.go:139` (`migrateRecordCtx`) | infers `Domain` for pre-`Domain` records | default to `memories` |
| `ingest.go:27` (`DeleteByCategory`) | rejects harvested categories | delete the guard |

### The consequence Option 4 forces: `list standards` must be reimplemented

This is the finding that shapes the work, and it is not obvious from the tool
surface.

`handleListStandardsCategories` (`internal/server/handlers_standards.go:18`) is
built **entirely** on `ListStandardsOverview`, which scans only three secondary
indexes — `_idx:cat:harvestedcode:`, `_idx:cat:packagedoc:`, `_idx:cat:sysdrift:`
— and then discards any record whose key does not begin with `pkg:`
(`badger.go:3111-3113`). `list projects` has the identical shape through
`ListDomainOverview`.

`save_to_recall` writes standards with a caller-supplied category (defaulting to
`ServerID`, `server.go:485-488`) under a key of the form
`<serverID>:standards:<nanos>` (`server.go:481`). It never writes
`HarvestedCode` and never writes a `pkg:` key.

**Therefore `list standards` and `list projects` are already blind to every
record `save_to_recall` has ever written.** They only ever listed harvested
symbols. This is a pre-existing defect, independent of this MADR, and deleting
the scanners without replacement would turn both tools into permanent empty
responses.

So the overview layer is **rewritten, not merely deleted**:

* `ListStandardsOverview` and `ListDomainOverview` are reimplemented over the
  existing `_idx:domain:<domain>:` index — the same index `VacuumProjects`
  already uses (`badger.go` `VacuumProjects`) — grouping by category and key
  rather than by Go package path.
* `StandardsPackageOverview` loses its Go-shaped fields (`ByType`,
  `HasPackageDoc`, `Checksum`, and `StandardsSymbolSummary.SymbolType`) in
  favour of a category/key grouping that describes what the store actually
  holds.
* `VacuumStandards` is rewritten to mirror `VacuumProjects`, which is already a
  clean domain-index scan with no harvest coupling. Its only current function is
  reporting orphaned `SysDrift` keys, which will no longer exist.

This is a genuine fix rather than a workaround: after it, both tools list the
records that are actually in their namespace for the first time.

**Retained:** the symbol-shaped *search* filters (`SymbolType`, `Interface`,
`Receiver`) in `SearchStandards`/`SearchProjects`. They are implemented as plain
tag matches — `Interface` becomes `"implements:"+q.Interface`
(`badger.go:3236`, `:3273-3274`) — so they work against any record whose tags
carry those prefixes and cost nothing to keep.

**Retained:** `ingest_files` / `MemoryStore.ProcessPath`
(`internal/memory/ingest.go:444`), the toolchain-free ingestion path, unchanged.

### Consequences

* Good, because the binary no longer requires, searches for, or executes an
  external `go` at any point. `MCP_GO_BIN_PATH` ceases to exist as a concept.
* Good, because 19 `golang.org/x/tools` packages and `golang.org/x/mod` leave
  the dependency graph, along with ~2,100 lines of code and test.
* Good, because no MCP tool can any longer trigger `go get` against a remote
  module, write to the user's module cache, or fetch an arbitrary URL.
* Good, because `os.Setenv("PATH", ...)` mutation of the running server process
  (`gobin.go:141-155`) is eliminated.
* Good, because three lies are retired at once: the `harvest` tool description,
  the `RECALL_GO_BIN` error text, and the unmapped `harvest.categories` /
  `excludes` / `extensions` template keys.
* **Bad, because Go structural intelligence is lost.** There is no replacement
  for AST-derived symbol signatures, receivers, interface satisfaction,
  dependency lists, doc-comment linkage, examples, or `go doc -all` package
  overviews. Users who relied on `harvest standards encoding/json` have no
  in-tree equivalent; `ingest_files` indexes file text, not structure.
* **Bad, because web-page ingestion is lost.** `ScrapeWebDocument` was the only
  path that pulled a remote URL into the store. This is deliberate — it was
  reachable only as an undocumented fallback from a failed module fetch (`resolver.go:99`, `:120`) — but it
  is a capability removal, not merely a refactor.
* Bad, because this is a breaking change to the CLI and to the
  `safetools_internal` schema. Existing `recall.yaml` files listing `harvest`
  must be handled: see "Compatibility" below.
* **Bad, because previously harvested records become unreachable orphans.**
  Records in `HarvestedCode`, `PackageDoc`, and `SysDrift` are not migrated, not
  deleted, and no longer recognised by any code path. They keep occupying the
  datastore, they no longer appear in any overview, and `delete --category
  HarvestedCode` no longer has a category filter that admits them. This is the
  cost Option 3 existed to avoid, and the operator has accepted it explicitly.
  Operators who want the space back should `purge` the affected namespace.
* Good, because `list standards` and `list projects` start listing what is
  actually in those namespaces. Rewriting the overview over the domain index
  fixes a pre-existing blindness to every `save_to_recall` record.
* Bad, because that rewrite changes the *shape* of the `list standards` /
  `list projects` response — package-grouped symbol listings become
  category-grouped key listings. Any client parsing the old text format breaks.
* Neutral, because `standards`/`projects` remain first-class writable namespaces
  through `save_to_recall` and `update_in_recall`.
* Neutral, because deleting the `Save` guard at `badger.go:898` means a
  `memories` write may now use the string `"HarvestedCode"` as a category. With
  the standards-domain meaning gone it is just an ordinary category name, and
  the associated `RecordSecurityViolation()` counter loses one trigger.

### Compatibility

An existing `recall.yaml` will contain `- harvest` under `safetools_internal`
and a `harvest:` block. Neither breaks startup, and both behaviours are pinned
by regression tests:

1. **Unknown safe-tool names are already ignored.** `add()` returns early when
   `safeMap[name]` has no matching registration (`registration.go:25-27`), and
   `RegisterSafeToolsInternal` only ever consults the map for names the code
   registers (`registration.go:130-137`). A stale `- harvest` entry is therefore
   inert. A regression test must lock this in rather than leave it assumed.
2. **An unknown `harvest:` YAML block does not fail the load — verified.**
   `internal/config/config.go:220` calls plain `c.v.Unmarshal(&newState)` with
   no `DecoderConfig`, no `ErrorUnused`, and no `UnmarshalExact`, so
   mapstructure's default of ignoring unmapped keys applies. This was confirmed
   empirically against the exact pinned version (viper v1.21.0, `go.mod:17`): a
   YAML document carrying an orphan `harvest:` block, `harvest_chunk_size` and
   `harvest_inter_batch_sleep_ms` under `batchsettings`, and `- harvest` under
   `safetools_internal` unmarshalled cleanly into a struct with no
   corresponding fields, with `max_batch_size` still read correctly and the
   stale `safetools_internal` entry still readable as data. Upgraded installs
   will not fail to start. The plan re-verifies this against the real loader.

**Datastore compatibility is explicitly not provided.** Per the operator's
decision there is no migration, no back-compatible read path, and no cleanup
pass. Records written by v1.0.0-v1.1.1 in the three harvested categories are
left in place as inert data. Badger will still open, read, and serve them by
exact key through `get`, because `get` resolves a key directly rather than
through a category index — but nothing enumerates them any more. The release
note must say this plainly.

### Confirmation

The change is confirmed when all of the following hold:

1. `grep -rin "harvest" --include="*.go" cmd/ internal/` returns **zero**
   results — including `internal/memory/`, whose category constants and their
   consumers are deleted under Option 4.
2. `grep -rn "exec.Command\|exec.LookPath\|go/packages\|go/ast\|go/types\|GOROOT\|GOPATH\|MCP_GO_BIN_PATH\|RECALL_GO_BIN" --include="*.go" cmd/ internal/`
   returns **zero** results.
3. `go list -deps ./... | grep -c golang.org/x/tools` returns `0`, and `go.mod`
   contains no `golang.org/x/tools` or `golang.org/x/mod` line after
   `go mod tidy`.
4. `go build ./...`, `go vet ./...`, `go test ./...`, and
   `golangci-lint run -c .golangci.yml ./...` all pass. (Baseline recorded
   2026-08-29: `go vet ./...` exits 0 and `go test ./internal/harvest/...
   ./internal/server/...` passes, so any failure is attributable to this change.)
5. `mcp-server-recall harvest --help` exits non-zero with cobra's unknown-command
   error, and `harvest` is absent from `mcp-server-recall --help`.
6. A freshly generated `recall.yaml` contains no `harvest:` block, no
   `harvest_chunk_size`, no `harvest_inter_batch_sleep_ms`, and no `- harvest`
   under `safetools_internal`.
7. A server started against a **pre-existing** `recall.yaml` that still contains
   `- harvest` and a `harvest:` block starts cleanly, registers its remaining
   internal tools, and logs no error.
8. Against a datastore seeded with **both** legacy harvested records and
   `save_to_recall`-written standards: `list standards` and `list projects`
   return the `save_to_recall` records (which they do **not** today), `search`
   with `symbol_type` still matches on the `type:` tag, and `prune_records`
   with `namespace=standards` completes without error. Legacy harvested records
   being absent from the listings is the expected, accepted outcome — not a
   regression to investigate.
9. The `dash` TUI shows a "Taxonomy & Categories" tab carrying the Taxonomy and
   Category Distribution panels but no "AST Ingestion Pipeline" panel; the tab
   **count is unchanged**, and navigating every tab end to end renders the
   correct page for each label (guarding the `iota`/`navItems` pairing).
10. `make build-all` produces all four platform binaries.

## Documentation impact

Removal is not complete while the guides still describe the feature. The
following must be rewritten in the same change:

| File | Lines | Required change |
|---|---|---|
| `README.md` | 4, 72, 102, 122, 164, 167 | Drop "Go source harvesting" from the summary; remove `harvest` from the command lists; remove the Go-executable requirement and the harvest-settings caveat. |
| `docs/guides/cli-reference.md` | 13-14, 98-136, and the "Go executable resolution" section | Delete both matrix rows, both command sections, and the entire resolution-order section. |
| `docs/guides/mcp-tools.md` | 25, 254-261 | Delete the `harvest` matrix row and its section; retitle "Harvest and ingest" to cover `ingest_files` only. |
| `docs/guides/configuration.md` | 57, 67-68, 72-…, 103-107, 123-125, 144-148, 239 | Remove all harvest keys from both examples, the settings table, the ignored-keys table, and `MCP_GO_BIN_PATH` from the environment table. |
| `docs/guides/client-integration.md` | 82, 122, 158 | Remove `MCP_GO_BIN_PATH` guidance and drop `harvest` from the list of CLI commands that require a running server. |
| `docs/guides/platform-installation.md` | 19, 135, 238, 413 | Go becomes required only to build from source; delete the `MCP_GO_BIN_PATH` troubleshooting row. |
| `docs/guides/getting-started.md` | 123, 133 | Replace the harvest walkthrough with an `ingest_files` / `save_to_recall` example. |
| `docs/guides/operations-and-security.md` | 239 | Remove "external Go modules and harvested inputs" from the trust-boundary discussion. |
| `docs/guides/repository-assessment.md` | 62, 66, 73, 92, 99, 235 | Mark the harvest rows as removed by this MADR and strike the `RECALL_GO_BIN` debt item as resolved by deletion. |

## More Information

* Supersedes nothing. Amends no prior decision: 0001–0004 concern config
  round-tripping, datastore init, the installer, and `configure`, none of which
  reference harvest.
* Blast radius by file count under Option 4: **8 files deleted**, **16 files
  edited** — the 11 of Option 3 (`registration.go`,
  `handlers_consolidated.go`, `handlers_structs.go`, `server/constants.go`,
  `config.go`, `config_template.go`, `snapshot.go`, `dashboard.go`,
  `dashboard_views.go`, `go.mod`, `go.sum`) plus five the storage-layer
  deletion adds: `internal/memory/badger.go`, `internal/memory/constants.go`,
  `internal/memory/record.go`, `internal/memory/ingest.go`, and
  `internal/server/handlers_standards.go` / `handlers_projects.go` (which
  consume the overview structs).
* Test updates: `internal/server/server_test.go` (`TestUniversalHarvest`,
  285-294, and the stanza at 347-350),
  `internal/server/server_coverage_test.go` (88-89, 198-199, plus the
  `HarvestedCode` fixtures at 69-70 and delete assertions at 264, 269),
  `internal/config/accessors_test.go` (36, 53-57),
  `internal/config/config_test.go` (26),
  `cmd/mcp-server-recall/cmd_test.go` (14-16),
  `cmd/mcp-server-recall/cmd_all_extra_test.go` (14, 30), and
  `cmd/mcp-server-recall/dashboard_views_test.go` (51, 133).
* Unlike Option 3, the `internal/memory` tests that write `HarvestedCode`
  records — `memory_extra_test.go` (10 sites), `badger_domain_test.go` (6),
  `ingest_test.go` (1), `badger_vacuum_test.go` (1) — **must be rewritten**.
  They assert the category-filtered behaviour this option deletes.
* This is a user-visible breaking change — two CLI commands, one MCP tool, a
  configuration block, the response shape of `list standards`/`list projects`,
  and the reachability of previously harvested records. It warrants a minor
  version bump from v1.1.1 with an explicit removal note stating that legacy
  harvested records are left in place but are no longer enumerated, and that
  `purge` is the way to reclaim the space.
* An implementation plan
  (`0005-PLAN-remove-harvest-and-go-toolchain-dependency.md`) must be written
  and approved before any code changes begin.
