---
status: "accepted"
date: 2026-08-29
decision-makers: maccavelli
pairs-with: 0005-MADR-remove-harvest-and-go-toolchain-dependency.md
---

# 0005-PLAN: Remove the harvest subsystem and every dependency on an external Go toolchain

Implementation plan for
[0005-MADR](0005-MADR-remove-harvest-and-go-toolchain-dependency.md). Execution
begins only after the operator approves **this document**. Every line-number
citation below was verified against the working tree on 2026-08-29.

## Baseline (captured 2026-08-29, before any edit)

| Check | Result |
|---|---|
| `git status --porcelain` | clean but for the two `0005-*` docs |
| `go vet ./...` | exit 0 |
| `go test ./internal/harvest/... ./internal/server/...` | pass |
| `go list -deps ./... \| grep -c golang.org/x/tools` | 19 |
| `grep -rlc "internal/harvest" --include="*.go"` | 2 files (`handlers_harvest.go`, `handlers_harvest_test.go`) |

Any failure after a phase that is not explained by that phase is a **deviation**
— stop and prompt per the deviation protocol at the end of this document.

## Phase ordering rationale

Eight phases, ordered so the tree **compiles and tests green at every commit**.
The two importers of `internal/harvest` (Phases 1 and 2) are severed before the
package itself is deleted (Phase 3); if the order were reversed, Phases 1-2
would each land a broken build.

Phases 6 and 7 carry out the operator's Option 4 decision — delete the
harvested-category storage layer, no migration, no retention. They are split
because Phase 6 is mechanical deletion of guards and filters while Phase 7 is a
substantive reimplementation of `list standards`, `list projects`, and
`VacuumStandards`. They are nonetheless **coupled**: the tree does not build
with 6 applied and 7 absent, so they land together or not at all. Treat them as
one unit when reverting.

---

## Phase 1 — Sever the MCP tool binding

**Goal:** remove the `harvest` MCP tool and its handler layer. After this phase
`internal/harvest` still exists on disk but has zero importers.

### Files

| File | Action |
|---|---|
| `internal/server/handlers_harvest.go` | **delete** (266 lines) |
| `internal/server/handlers_harvest_test.go` | **delete** (154 lines) |
| `internal/server/registration.go` | edit |
| `internal/server/handlers_consolidated.go` | edit |
| `internal/server/handlers_structs.go` | edit |
| `internal/server/constants.go` | edit |
| `internal/server/server_test.go` | edit |
| `internal/server/server_coverage_test.go` | edit |

### Steps

1. `git rm internal/server/handlers_harvest.go internal/server/handlers_harvest_test.go`
2. `registration.go`: delete the registration block at `:77-79`
   (`if safeMap != nil && safeMap["harvest"] { add(...) }`) and remove the
   `"harvest"` element from the `ReloadTools` name slice at `:101`.
3. `handlers_consolidated.go`: delete `UniversalHarvestInput` (`:78-84`) and
   `handleUniversalHarvest` (`:229-238`).
4. `handlers_structs.go`: delete `HarvestStandardsInput` and
   `HarvestProjectsInput` (`:18-30`), and delete the already-dead
   `HarvestInput` (`:140-145`) — grep confirms it has no referent anywhere,
   including tests.
5. `constants.go`: delete `harvestAuth`, `harvestDatabase`, `harvestTest`
   (`:33-35`). Their sole consumer was `detectDomainTags` in the deleted file;
   leaving them trips the `unused` linter.
6. `server_test.go`: delete `TestUniversalHarvest` (`:285-294` inclusive of its
   closing brace) and the "UniversalHarvest" stanza at `:347-350`.
7. `server_coverage_test.go`: delete the `handleUniversalHarvest` calls and
   their comments at `:88-89` and `:198-199`. Leave the `HarvestedCode` saves at
   `:69-70` and the delete assertions at `:264` and `:269` — those exercise the
   legacy storage paths the MADR retains.

### Verification

```bash
go build ./... && go vet ./...
CGO_ENABLED=0 go test ./internal/server/...
grep -rn "arvest" internal/server/ | grep -v HarvestedCode   # expect: no output
grep -rn "internal/harvest" --include="*.go" .               # expect: no output
```

### Acceptance

* `internal/harvest` has zero importers.
* `internal/server` contains no `harvest` identifier other than the
  `HarvestedCode` category string in tests.
* Server tests pass.

**Commit:** `refactor(server): remove harvest tool binding and handler layer`

---

## Phase 2 — Remove the CLI commands

**Goal:** `harvest`, `harvest standards`, and `harvest projects` cease to exist.

### Files

| File | Action |
|---|---|
| `cmd/mcp-server-recall/harvest.go` | **delete** (94 lines) |
| `cmd/mcp-server-recall/harvest_test.go` | **delete** (20 lines) |
| `cmd/mcp-server-recall/cmd_test.go` | edit |
| `cmd/mcp-server-recall/cmd_all_extra_test.go` | edit |

### Steps

1. `git rm cmd/mcp-server-recall/harvest.go cmd/mcp-server-recall/harvest_test.go`.
   This removes `harvestCmd`, `harvestStandardsCmd`, `harvestProjectsCmd`,
   `runHarvestViaMCP`, and the `RootCmd.AddCommand(harvestCmd)` in that file's
   `init()`.
2. `cmd_test.go`: delete the `harvestCmd == nil` assertion (`:14-16`).
3. `cmd_all_extra_test.go`: remove `{"harvest", "repo"}` (`:14`) from
   `TestOtherCommands` and `{"harvest", "memories"}` (`:30`) from
   `TestAllCommands`.
4. Add a positive regression test — `TestHarvestCommandIsGone` — asserting
   `RootCmd.Find([]string{"harvest"})` does not resolve to a command named
   `harvest`. Removal is otherwise only provable by absence, and
   `cmd.Execute()` in the coverage lists swallows unknown-command errors.

`mcplib.NewRecallClient` / `WaitForRecallServer` stay in the module — `export.go`,
`import.go`, and `prune.go` still use them. Do **not** touch `go.mod`'s `mcplib`
line in this phase.

### Verification

```bash
go build ./... && CGO_ENABLED=0 go test ./cmd/...
go run ./cmd/mcp-server-recall --help | grep -c harvest      # expect: 0
go run ./cmd/mcp-server-recall harvest --help; echo "exit=$?" # expect: non-zero
grep -rn "arvest" cmd/                                        # expect: no output
```

### Acceptance

* `harvest` absent from `--help`; invoking it exits non-zero.
* `cmd` package tests pass.

**Commit:** `refactor(cli): remove harvest standards and harvest projects commands`

---

## Phase 3 — Delete the package and drop the toolchain dependency

**Goal:** the Go-toolchain dependency leaves the repository and the binary.

### Files

| File | Action |
|---|---|
| `internal/harvest/` (9 files, 1,275 lines) | **delete** |
| `go.mod`, `go.sum` | edit via `go mod tidy` |

### Steps

1. `git rm -r internal/harvest`
2. Remove `golang.org/x/tools v0.45.0` from the direct `require` block
   (`go.mod:19`).
3. `go mod tidy` — this should also drop `golang.org/x/mod` from the indirect
   block. Do not hand-edit the indirect block; let tidy own it.

### Verification

```bash
go build ./... && go vet ./...
go list -deps ./... | grep -c golang.org/x/tools     # expect: 0
grep -nE "golang.org/x/(tools|mod)" go.mod           # expect: no output
grep -rn "exec.Command|exec.LookPath|go/packages|go/ast|go/types|GOROOT|GOPATH|MCP_GO_BIN_PATH|RECALL_GO_BIN" \
  --include="*.go" cmd/ internal/                    # expect: no output
CGO_ENABLED=0 go test ./...
# CI parity: tidy must be a no-op
cp go.mod /tmp/m && cp go.sum /tmp/s && go mod tidy && diff -q go.mod /tmp/m && diff -q go.sum /tmp/s
```

### Acceptance

* Zero `golang.org/x/tools` packages in the dependency graph (was 19).
* Zero `exec`/`go/*`/toolchain-env references in `cmd/` and `internal/`.
* `go mod tidy` is idempotent (CI gate at `.github/workflows/ci.yml:72-83`).

**Commit:** `refactor(harvest): delete internal/harvest and drop golang.org/x/tools`

---

## Phase 4 — Remove the configuration surface

> **Deviation D-1 (2026-08-29): this phase now runs _after_ Phase 5.** See the
> Deviation log. The ordering below is preserved as originally approved;
> ~~Phase 4 → Phase 5~~ is executed as **Phase 5 → Phase 4**.

**Goal:** no harvest knobs in the loader, the accessors, or generated YAML.

### Files

| File | Action |
|---|---|
| `internal/config/config.go` | edit |
| `cmd/mcp-server-recall/config_template.go` | edit |
| `internal/config/accessors_test.go` | edit |
| `internal/config/config_test.go` | edit |

### Steps — `internal/config/config.go`

1. Delete `BatchConfig.HarvestChunkSize` and `BatchConfig.HarvestInterBatchSleepMs`
   (`:37-38`). Keep `MaxBatchSize`, `IngestInterBatchSleepMs`,
   `LoadFastWritesEnabled` — all have live consumers.
2. Delete the `HarvestConfig` type (`:44-47`) and the `State.Harvest` field
   (`:63`).
3. Delete the defaults at `:126-127` (`batchsettings.harvest_chunk_size`,
   `batchsettings.harvest_inter_batch_sleep_ms`) and `:132-133`
   (`harvest.exclude_dirs`, `harvest.disable_drift`).
4. Delete `"harvest"` from the default `safeToolsInternal` slice (`:165`).
5. Delete the accessors `ExcludeDirs()` (`:340-346`) and `HarvestDisableDrift()`
   (`:383-388`).
6. `:212` — the reload log line keys on `disable_drift_applied_status` via
   `cfg.HarvestDisableDrift()`. Reduce it to
   `slog.Info("[Viper] Configuration reloaded into memory")`; do not invent a
   replacement field.

### Steps — `cmd/mcp-server-recall/config_template.go`

7. Remove `- harvest` from `safetools_internal` (`:42`).
8. Remove `harvest_chunk_size` and `harvest_inter_batch_sleep_ms` (`:127-128`).
9. Remove the entire `harvest:` block, `:149-183` (through `- .rs`; the closing
   backtick is `:184` and must be preserved). This also deletes
   `harvest.categories`, `harvest.excludes`, and `harvest.extensions`, which
   have never mapped to any struct field and are already documented as ignored
   (`docs/guides/configuration.md:123-125`).

### Steps — tests

10. `accessors_test.go`: delete the `HarvestDisableDrift()` call (`:36`) and the
    `HarvestChunkSize` / `HarvestInterBatchSleepMs` assertions (`:53-57`).
    ~~(as approved)~~ — **amended by D-1:** this list was incomplete. Also
    remove the three `c.ExcludeDirs()` calls at `:38` and `:80-81`, which the
    approved step did not name.
11. `config_test.go`: delete the `HarvestDisableDrift()` call (`:26`).

### Verification

```bash
go build ./... && CGO_ENABLED=0 go test ./internal/config/... ./cmd/...
grep -rn "arvest" internal/config/ cmd/mcp-server-recall/config_template.go  # expect: no output
# Generated file is clean:
TMP=$(mktemp -d) && go run ./cmd/mcp-server-recall configure --help >/dev/null
grep -nE "harvest|harvest_chunk_size|harvest_inter_batch_sleep_ms" "$TMP"/recall.yaml 2>/dev/null || echo "clean"
```

### Acceptance

* No `harvest` identifier in `internal/config` or the template.
* A freshly generated `recall.yaml` contains no `harvest:` block, neither
  `batchsettings` harvest key, and no `- harvest` under `safetools_internal`.

**Commit:** `refactor(config): remove harvest settings, accessors and template block`

---

## Phase 5 — Telemetry and dashboard

> **Deviation D-1 (2026-08-29): this phase now runs _before_ Phase 4.** Its
> contents are unchanged.

**Goal:** stop reporting AST statistics that no longer have a source.

### Files

| File | Action |
|---|---|
| `internal/telemetry/snapshot.go` | edit |
| `cmd/mcp-server-recall/dashboard.go` | edit |
| `cmd/mcp-server-recall/dashboard_views.go` | edit |
| `cmd/mcp-server-recall/dashboard_views_test.go` | edit (3 known sites) |

### Steps — telemetry

1. Delete the `ASTStats` type (`:78-81`), the `AST ASTStats` field on the
   snapshot struct (`:110`), and its population block (`:262-265`). The
   `ParsedFiles` value there was never measured — it is
   `metrics.Namespaces[DomainProjects] * 2`, self-described as a "Heuristic
   mapping" — so nothing observable is lost.

### Steps — dashboard (handle with care)

`renderTaxonomyAST` (`dashboard_views.go:384-453`) renders **three** panels and
only the first is harvest-related:

| Panel | Lines | Action |
|---|---|---|
| "AST Ingestion Pipeline" (`snapshot["ast"]`) | 388-399 | delete |
| "Taxonomy & Tag Distribution" (`snapshot["taxonomy"]`) | 401-426 | **keep** |
| "Category Distribution (Top 10)" (`snapshot["categories"]`) | 429-450 | **keep** |

2. Delete lines 388-399 (`stAst` through the `astBox := ...` assignment).
3. Change `:428` from
   `b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, astBox, taxBox))` to
   `b.WriteString(taxBox)`.
4. Rename `renderTaxonomyAST` → `renderTaxonomy` and update its call site
   (`dashboard.go:325`).
5. Rename the tab constant `tabTaxonomyAST` → `tabTaxonomy`
   (`dashboard.go:59`, `:324`) and relabel `navItems` entry
   "Taxonomy & AST Pipeline" → "Taxonomy & Categories" (`:71`).
6. `dashboard_views_test.go` — three known sites:
   * `:51` — drop the `"ast"` key from the fixture snapshot; it becomes dead
     fixture data once the panel is gone.
   * `:133` — update `m.activeTab = tabTaxonomyAST` to the renamed constant.
   * `:136` — the assertion is on `'Category Distribution'`, a panel this plan
     **keeps**, so it must continue to pass unchanged. If it fails, the trim in
     step 2 cut too far.

   Note the tab loops at `:93`, `:103` and `:149` iterate
   `tabOverview..tabQuit` as a range rather than asserting fixed index values,
   so they tolerate the rename — and correspondingly would *not* have caught a
   renumbering had the constant been deleted. That is why manual tab-walking
   stays in the verification list.

> **Do not delete the tab constant.** It is an `iota` member whose ordering is
> mirrored positionally by the `navItems` slice. Removing it silently renumbers
> `tabRPCAnalytics`, `tabNetwork`, `tabSecurity`, `tabConfig` and `tabQuit`, and
> every nav label after it would render the wrong page. Renaming keeps all
> indices stable.

### Verification

```bash
go build ./... && CGO_ENABLED=0 go test ./cmd/... ./internal/telemetry/...
grep -rn "ASTStats|tabTaxonomyAST|renderTaxonomyAST|AST Ingestion" cmd/ internal/  # expect: no output
grep -rn 'snapshot\["taxonomy"\]|snapshot\["categories"\]' cmd/                     # expect: still present
```

Then a manual pass, since the TUI has no automated navigation coverage:

```bash
go run ./cmd/mcp-server-recall dash
```

Step through **every** tab left-to-right and confirm each label renders its own
page — this is the guard for the `iota`/`navItems` pairing.

### Acceptance

* No `ASTStats` in the telemetry payload; snapshot still marshals.
* The dashboard shows "Taxonomy & Categories" with the Taxonomy and Category
  Distribution panels and no AST panel.
* Tab **count is unchanged**; every tab renders its correct page.

**Commit:** `refactor(telemetry,dash): drop AST stats and the AST dashboard panel`

---

## Phase 6 — Delete `HarvestedCategories` and its simple consumers

**Goal:** remove the harvested-category concept from the storage layer. This
phase covers the deletions that are one-line guards, filters, or defaults;
Phase 7 handles the two subsystems that need reimplementation.

> **This phase supersedes the Option 3 "comments only" phase.** Per the
> operator's 2026-08-29 decision there is no retention and no migration.

### Files

| File | Action |
|---|---|
| `internal/memory/badger.go` | edit (7 sites) |
| `internal/memory/constants.go` | edit |
| `internal/memory/record.go` | edit |
| `internal/memory/ingest.go` | edit |
| `internal/memory/memory_extra_test.go`, `badger_domain_test.go`, `ingest_test.go`, `badger_vacuum_test.go` | rewrite affected assertions |
| `internal/server/server_coverage_test.go` | edit |

### Steps

1. `badger.go:35-41` — delete the `HarvestedCategories` var and its doc comment.
2. `constants.go:5-6` — delete `catHarvestedCode` and `catSysDrift`. Verify no
   other referent survives; `catSysDrift` is also used at `badger.go:1878` and
   `:3230`, both handled in this phase or Phase 7.
3. `badger.go:898` (`Save`) — delete the memory-domain rejection guard and its
   `RecordSecurityViolation()` call. With the standards-domain meaning gone,
   `"HarvestedCode"` is an ordinary category string.
4. `badger.go:1964` (`searchByTag`) — delete the `!HarvestedCategories[...]`
   condition, keeping the `getErr == nil` check.

   > **Check for a leak.** `searchByTag` walks a tag index that is not domain
   > scoped, so this filter may have been the only thing keeping standards
   > records out of memory-scoped tag search. Before deleting, confirm against
   > the caller whether domain scoping is applied elsewhere. If it is not,
   > replace the category filter with a `rec.Domain == DomainMemories` check
   > rather than removing it outright — and record that as a deviation, because
   > it is a behaviour decision this plan did not anticipate.

5. `badger.go:2789` (`SaveBatch`) — replace the category-based domain inference
   with `rec.Domain = DomainMemories` when `e.Domain == ""`.
6. `badger.go:2880` (`syncBatchToSearchIndex`) — same change for the synthetic
   `domain:` index tag.
7. `badger.go:3028`, `:3047` (`ListCategories`) — delete the exclusion filter so
   the loop counts every category, and update the doc comment at `:3028` which
   names the three excluded categories.
8. `badger.go:3801`, `:3864`, `:3916` (`DeleteStandards`, `DeleteProjects`) —
   these currently use `HarvestedCategories` as an **inclusion** filter, i.e.
   they refuse to delete a category that is not harvested. Invert to accept any
   category within the target domain, gating on `rec.Domain` instead.
9. `record.go:139` (`migrateRecordCtx`) — replace the category-based `Domain`
   inference for pre-`Domain` records with `rec.Domain = DomainMemories`.
10. `ingest.go:23-27` (`DeleteByCategory`) — delete the rejection guard and the
    doc line describing it.
11. Rewrite the memory tests that assert the deleted behaviour. These currently
    pass *because* of the filters being removed, so they will fail loudly:
    `memory_extra_test.go` (10 `HarvestedCode` sites), `badger_domain_test.go`
    (6), `ingest_test.go:117`, `badger_vacuum_test.go:202`. Convert each to use
    an ordinary category and assert the new domain-gated behaviour. Do **not**
    delete a test to make it pass — if an assertion cannot be re-expressed, that
    is a deviation.
12. `internal/server/server_coverage_test.go` — the `HarvestedCode` fixtures at
    `:69-70` and the delete assertions at `:264`, `:269` now exercise the
    inverted filter; update them to an ordinary category.

### Verification

```bash
go build ./... && go vet ./...
CGO_ENABLED=0 go test ./internal/memory/... ./internal/server/...
grep -rn "HarvestedCategories|catHarvestedCode|catSysDrift" --include="*.go" internal/
#   expect: no output
```

### Acceptance

* `HarvestedCategories` and both category constants are gone.
* Memory and server tests pass with rewritten, not deleted, assertions.

**Commit:** `refactor(memory): delete HarvestedCategories and its guards`

---

## Phase 7 — Reimplement the overview and vacuum layers

**Goal:** `list standards`, `list projects`, and `prune_records` keep working —
and start working correctly. This is the substantive rewrite Option 4 forces.

### Why this is a rewrite and not a deletion

`handleListStandardsCategories` (`handlers_standards.go:18`) is built entirely
on `ListStandardsOverview`, which scans only `_idx:cat:harvestedcode:`,
`_idx:cat:packagedoc:`, `_idx:cat:sysdrift:` and discards any record whose key
lacks a `pkg:` prefix (`badger.go:3111-3113`). `list projects` is identical via
`ListDomainOverview`.

`save_to_recall` writes standards with a caller-supplied category under a
`<serverID>:standards:<nanos>` key (`server.go:481`, `:485-488`) — never
`HarvestedCode`, never a `pkg:` key. **Both tools are therefore already blind to
every record `save_to_recall` has written.** Deleting the scanners without
replacement would make them permanently empty.

### Files

| File | Action |
|---|---|
| `internal/memory/badger.go` | rewrite 3 regions |
| `internal/server/handlers_standards.go` | edit |
| `internal/server/handlers_projects.go` | edit |
| `internal/memory/badger_vacuum_test.go`, `badger_domain_test.go` | update |

### Steps

1. **Replace the overview types.** `StandardsPackageOverview`
   (`badger.go:3067-3074`) loses `ByType`, `HasPackageDoc`, and `Checksum`;
   `StandardsSymbolSummary` loses `SymbolType`. Replace with a category/key
   grouping that describes what the store actually holds — retain
   `TotalSymbols` (renamed to a record count) and the key list.
2. **Rewrite `ListStandardsOverview`** (`:3076-3096`) to scan
   `_idx:domain:standards:` — the same index `VacuumProjects` already uses —
   grouping by `rec.Category`. Delete `scanHarvestedCodeIndex` (`:3098-3155`),
   `scanPackageDocIndex` (`:3156-3182`), and `scanSysDriftIndex` (`:3183-3220`).
3. **Rewrite `ListDomainOverview`** (`:3336-3400`) the same way, dropping the
   `catHarvestedCode` / `PackageDoc` / `catSysDrift` switch at `:3375-3397`.
4. `badger.go:3230` (`recordMatchesDomainSearch`) — delete the
   `rec.Category == catSysDrift` early-return.
5. **Rewrite `VacuumStandards`** (`:1838-1912`). Its entire purpose is reporting
   orphaned `SysDrift` keys, which can no longer exist. Mirror `VacuumProjects`
   verbatim — a `_idx:domain:standards:` scan populating `report.TotalScanned` —
   so `prune_records namespace=standards` and the `all` path
   (`server.go:197`, `:244`) keep working. Keep the `VacuumReport` shape so both
   call sites are untouched.
6. `handlers_standards.go` — update `handleListStandardsCategories` for the new
   struct: the `SymbolType` post-filter (`:28-41`), the `HasPackageDoc` counters
   (`:50-52`, `:67-69`), and the `[%s] %s` symbol line (`:72`).
7. `handlers_projects.go` — the identical changes at `:27-33`, `:50`, `:67`,
   `:72`.
8. Leave `SearchStandards`/`SearchProjects` and their `SymbolType`, `Interface`,
   `Receiver` filters **untouched** — they are plain tag matches
   (`badger.go:3236`, `:3273-3274`) and cost nothing to keep.
9. Update `badger_vacuum_test.go` and `badger_domain_test.go` where they assert
   on package-grouped output or `SysDrift` orphan reporting.

### Verification

```bash
go build ./... && go vet ./...
CGO_ENABLED=0 go test ./internal/memory/... ./internal/server/...
grep -rn "scanHarvestedCodeIndex|scanPackageDocIndex|scanSysDriftIndex|HasPackageDoc" --include="*.go" internal/
#   expect: no output
```

Then the functional check that motivates the whole phase — against a scratch
datastore:

```text
save_to_recall  namespace=standards  key=STD-TEST-001  category=Testing
list            namespace=standards
```

`STD-TEST-001` **must appear**. It does not today. This is the acceptance test
for the rewrite.

### Acceptance

* `list standards` / `list projects` enumerate `save_to_recall` records.
* `prune_records` with `namespace=standards`, `namespace=projects`, and
  `namespace=all` all complete without error.
* No harvest-shaped scanner remains in `internal/memory`.

**Commit:** `refactor(memory): reimplement standards/projects overview over the domain index`

---

## Phase 8 — Documentation

**Goal:** no guide describes a command, tool, setting, or requirement that no
longer exists.

### Files and required changes

| File | Lines | Change |
|---|---|---|
| `README.md` | 4, 72, 102, 122, 164, 167 | Drop "Go source harvesting" from the summary; remove `harvest` from both command lists; delete the Go-executable requirement and the harvest-settings caveat. |
| `docs/guides/cli-reference.md` | 13-14, 98-115, 116-137, 138-150 | Delete both matrix rows, both command sections, and the whole "Go executable resolution" section (which ends where `## export` begins at `:151`). |
| `docs/guides/mcp-tools.md` | 25, 254-261 | Delete the `harvest` matrix row and its section; retitle "Harvest and ingest" → "Ingest". |
| `docs/guides/configuration.md` | 57, 67-68, 72+, 103-107, 123-125, 144-148, 239 | Remove harvest keys from both examples, the settings table and the ignored-keys table; drop `MCP_GO_BIN_PATH` from the environment table. |
| `docs/guides/client-integration.md` | 82, 122, 158 | Remove `MCP_GO_BIN_PATH` guidance; drop `harvest` from the list of CLI commands needing a running server. |
| `docs/guides/platform-installation.md` | 19, 135, 238, 413 | Go is now required **only** to build from source; delete the `MCP_GO_BIN_PATH` troubleshooting row. |
| `docs/guides/getting-started.md` | 123, 133 | Replace the harvest walkthrough with an `ingest_files` / `save_to_recall` example. |
| `docs/guides/operations-and-security.md` | 239 | Remove "external Go modules and harvested inputs" from the trust-boundary discussion. |
| `docs/guides/repository-assessment.md` | 62, 66, 73, 92, 99, 235 | Mark the harvest rows as removed by 0005; strike the `RECALL_GO_BIN` debt item as resolved by deletion. |

**Not edited:** `docs/0001`-`0004` MADR/PLAN files are a historical record and
stay as written. The single `harvest` hit in
`docs/0002-PLAN-configure-os-native-datastore-init.md:73` is incidental prose
about `os.UserHomeDir` path handling, unrelated to this subsystem.

### Verification

```bash
grep -rin "harvest|MCP_GO_BIN_PATH|RECALL_GO_BIN" README.md docs/guides/
#   expect: only historical/removal notes in repository-assessment.md
npx markdownlint-cli2 "README.md" "docs/guides/*.md"
```

### Acceptance

* No guide documents a `harvest` command, tool, or setting as available.
* Markdown lint clean.

**Commit:** `docs: remove harvest and Go-toolchain requirements from all guides`

---

## Final gate — run before declaring the work done

```bash
# 1. Nothing harvest-shaped anywhere in the tree (Option 4: no retained layer)
grep -rin "harvest" --include="*.go" cmd/ internal/
#    expect: no output

# 2. No toolchain dependency of any kind
grep -rnE "exec.Command|exec.LookPath|go/packages|go/ast|go/types|GOROOT|GOPATH|MCP_GO_BIN_PATH|RECALL_GO_BIN" \
  --include="*.go" cmd/ internal/
#    expect: no output

# 3. Dependency graph
go list -deps ./... | grep -c golang.org/x/tools     # expect: 0
grep -nE "golang.org/x/(tools|mod)" go.mod           # expect: no output

# 4. Full CI parity
gofmt -l $(git ls-files '*.go')
cp go.mod /tmp/m && cp go.sum /tmp/s && go mod tidy && diff -q go.mod /tmp/m && diff -q go.sum /tmp/s
go vet ./...
CGO_ENABLED=0 go test ./...
GOLANGCI_LINT="$(go env GOPATH)/bin/golangci-lint" make lint

# 5. CLI surface
go run ./cmd/mcp-server-recall --help | grep -c harvest    # expect: 0
go run ./cmd/mcp-server-recall harvest --help; echo $?     # expect: non-zero

# 6. Cross-platform build
make build-all
```

**Manual gates** (no automated coverage exists for these):

7. Start `serve` against a **pre-existing** `recall.yaml` that still carries
   `- harvest` under `safetools_internal` and a `harvest:` block. It must start
   cleanly, register its remaining internal tools, and log no error.
8. Against a scratch datastore, `save_to_recall` a standard and a project, then
   confirm `list standards` and `list projects` **return them** — they do not
   today. Then confirm `search` with `symbol_type` still matches on the `type:`
   tag, and `prune_records` succeeds for `standards`, `projects`, and `all`.
   Legacy harvested records not appearing is the accepted outcome, not a bug.
9. `dash` — walk every tab and confirm each label renders its own page.

## Pre-commit checks (every phase)

Per repository convention, before each phase's commit:

```bash
scripts/go-precheck.sh <each staged .go file>
```

This is `gofmt` + per-file `golint`, with the two known-allowed stutter warnings
(`config.ConfigDir`, `memory.MemoryStore`) filtered. A file that fails is not
committed until fixed. A `PreToolUse` hook enforces this at the tool layer.

## Deviation protocol

Anything this plan does not cover — a pre-existing defect surfaced by a new
test, a step that is wrong as written, a file that must be touched but is not
listed, or a MADR assumption the code contradicts — is a **deviation**. Stop,
present evidence (file, line, exact failure, and whether it is genuinely
pre-existing), and offer real fixes rather than workarounds. Once a resolution
is chosen, record it here as a dated deviation entry, amend the MADR if it
changes a decision or contradicts an asserted fact, and only then execute.

Specifically anticipated deviation risks:

* **Phase 5** — if `dashboard_views_test.go` asserts on the AST panel, that is a
  listed file to edit, not a deviation. If it asserts on tab **indices**, stop:
  that indicates the `iota` coupling is load-bearing in a way this plan has not
  accounted for.
* **Phase 6, step 4** — `searchByTag` walks a non-domain-scoped tag index. If
  the category filter was the only thing keeping standards records out of
  memory-scoped tag search, removing it leaks them. Substitute a
  `rec.Domain == DomainMemories` check and record the deviation.
* **Phase 6, step 11** — memory tests must be *rewritten*, never deleted, to go
  green. A test that cannot be re-expressed against the new behaviour is
  signalling that the behaviour change is wrong, not that the test is stale.
* **Phase 7** — if `list standards` still returns empty for a `save_to_recall`
  record after the rewrite, the domain index is not populated the way this plan
  assumes. Stop; do not paper over it by reinstating a key-prefix scan.
* **Phase 3** — if `go mod tidy` does not drop `golang.org/x/mod`, some other
  dependency needs it; leave it and note it, do not force-remove.

## Deviation log

### D-1 — 2026-08-29 — Phase 4 cannot compile before Phase 5

**Found during execution of Phase 4.**

Phase 4 deletes `Config.HarvestDisableDrift()` and `Config.ExcludeDirs()`
(`config.go:340-346`, `:383-388`). Their only non-test consumer is
`internal/telemetry/snapshot.go:263-264`, which **Phase 5** was scheduled to
delete. Running the approved order therefore landed a broken build:

```text
internal/telemetry/snapshot.go:263:22: cfg.HarvestDisableDrift undefined
internal/telemetry/snapshot.go:264:26: cfg.ExcludeDirs undefined
```

This violated the plan's own stated invariant that the tree compiles and tests
green at every commit. The defect is in the plan, not the code — the phase
contents were correct, only their order was wrong.

A second, related under-scoping surfaced with it: Phase 4 step 10 named only
`accessors_test.go:36` and `:53-57`, but `:38` and `:80-81` also call
`c.ExcludeDirs()`.

**Options considered:** (A) swap the two phases; (B) merge them into one commit;
(C) keep the order behind a temporary zero-value shim.

**Decision (operator, 2026-08-29): Option A.** Phase 5 executes before Phase 4.
Both phases keep their approved contents and commit messages; only the sequence
changes. Option B was declined for bundling two unrelated concerns — config
schema and observability UI — into one reviewable unit and losing the bisect
point. Option C was not recommended: it ships a deliberately dead accessor for
one commit solely to preserve a phase number.

**Files added to scope:** none. `internal/config/accessors_test.go` was already
in Phase 4's file list; only the line range within it grew.

**Consequence of doing nothing:** one commit in the history would not build,
breaking `git bisect` across the range and failing CI on that commit.

### D-2 — 2026-08-29 — `snapshot.go` has never passed the local precheck

**Found during execution of Phase 5.**

Staging `internal/telemetry/snapshot.go` for the Phase 5 commit failed
`scripts/go-precheck.sh`. Investigation confirmed the failure is **pre-existing
and not caused by this work**: `golint` against the unmodified `HEAD~1` copy
reports **15** findings; the post-Phase-5 file reports **14**. The single
difference is `ASTStats`, itself one of the undocumented exported types this
phase deletes — the change strictly reduced the count.

Every exported type in the file lacks a doc comment, plus a name stutter on
`TelemetrySnapshot`. The precheck allow-list exempts only `config.ConfigDir` and
`memory.MemoryStore`, so any earlier commit staging this file would have failed
too. Phases 1-3 passed only because none of them touched it.

CI was never affected: the CI gate is `golangci-lint` (clean); classic `golint`
is the local pre-commit tool only.

**Procedural note.** The Phase 5 commit initially landed *despite* the failing
check, because the executing command ran the precheck and `git commit` as
separate statements rather than chaining them. The commit was amended rather
than left in history.

**Options considered:** (A) fix the file — add the missing comments and rename
the stuttering type; (B) comments only, and add `TelemetrySnapshot` to the
precheck allow-list; (C) fix it in a separate commit, leaving Phase 5 as
approved.

**Decision (operator, 2026-08-29): Option A.** Added 12 type doc comments and
one var comment, and renamed `TelemetrySnapshot` → `Snapshot`. The rename is
contained to `snapshot.go` (declaration and one use, no external referents, no
collision) and does not change the JSON wire shape, which is driven by struct
tags — so the dashboard's `snapshot["taxonomy"]` / `["categories"]` reads are
unaffected. `golint` on the file is now clean: 15 findings → 0.

**Files added to scope:** none. `internal/telemetry/snapshot.go` was already in
Phase 5's file list; the edit within it grew.

**Consequence of doing nothing:** every later phase staging this file hits the
same gate, and a commit stays in history with an unchecked file.

**Unrelated observation, not acted on:** `TestRunServe_CanceledContext` failed
once during this phase with a Badger `mmap ... DISCARD: bad file descriptor`
error in a temp dir, then passed on two consecutive re-runs. Treated as a
pre-existing environmental flake — this phase touched only comments and a
package-internal type name. Recorded here rather than chased.

### D-3 — 2026-08-29 — `searchByTag` relied on the category filter for domain isolation

**Found during execution of Phase 6, step 4** — the risk the plan flagged in
advance, confirmed real.

`MemoryStore.Search` is memory-scoped. Its no-tag path, `searchGeneral`, is
domain-scoped (`prefix := _idx:domain:memories:`) and carries a comment saying it
was migrated off a `HarvestedCategories` filter. Its tag path, `searchByTag`,
was never migrated: it walks `_idx:tag:<tag>:`, which has **no domain scoping**,
and `!HarvestedCategories[rec.Category]` was the only isolation.

Deleting that filter outright would leak standards and projects records into
memory-scoped tag search. **It is in fact a wider pre-existing bug:** because
the filter was category-based, records in `sessions`, `server_status`,
`dialectic_history`, `documents` and `ecosystem` — none of them harvested
categories — already leak into memory tag search today.

**Resolution (as prescribed by the approved plan):** replace the category filter
with `rec.Domain == DomainMemories`, matching `searchGeneral`. This both
preserves the harvest-era isolation and closes the wider leak.

**Files added to scope:** none.

### D-4 — 2026-08-29 — `DeleteStandards` had no domain gate, and `category_number` was mis-wired

**Found during execution of Phase 6, step 8 and Phase 7, step 6.**

Two defects surfaced that the plan did not anticipate:

1. **`DeleteStandards`' category branch never checked the domain.** It appended
   every key from `_idx:cat:<category>:` unconditionally; the up-front
   `HarvestedCategories[category]` allowlist was its only protection. Removing
   that allowlist as Phase 6 specifies would have let
   `DeleteStandards(ctx, "SomeCategory", "")` delete records in **any** domain
   sharing that category name. `DeleteProjects` already gated on
   `rec.Domain == DomainProjects`; `DeleteStandards` did not.

   **Resolution:** gate both branches of `DeleteStandards` on
   `rec.Domain == DomainStandards`, matching `DeleteProjects`.

2. **`category_number` resolved against a package-grouped listing.** Both delete
   handlers sorted the overview's keys and passed the Nth as the *package*
   filter. With the overview now category-grouped, that would pass a category
   name into the package argument.

   **Resolution:** `category_number` now resolves to a category and feeds the
   category argument — which is what the parameter's name always implied. The
   list inputs lose `SymbolType` (meaningless without harvested symbols) and
   gain `Category`; `UniversalListInput` gains a `Category` field.

**Files added to scope:** `internal/server/handlers_structs.go` and
`internal/server/handlers_consolidated.go` were already in Phase 1's list but
are edited again here for the list-input schema change.

**Note on phase landing:** Phases 6 and 7 were committed as a single commit.
The plan states they are coupled and "land together or not at all" — the tree
does not build with 6 applied and 7 absent, and a non-building commit would
break the plan's own compile-green invariant.

## Rollback

Each phase is a single commit and reverts cleanly with `git revert`. Phase 3 is
the only one that touches `go.mod`/`go.sum`; reverting it restores the
dependency but leaves Phases 1-2 removed, which still builds (the package would
be present and unimported). Phases 6 and 7 are coupled — reverting 7 alone
leaves the overview layer calling deleted symbols, so revert both or neither.

No phase writes to, migrates, or deletes datastore contents, so no phase can
damage stored data; the loss under Option 4 is of *reachability*, introduced by
code change alone and fully undone by reverting Phases 6-7.

## Out of scope

* Any replacement for Go structural extraction. There is none, by decision.
* Any migration or cleanup of existing `HarvestedCode`, `PackageDoc`, or
  `SysDrift` records. Per the operator's decision they are left in the datastore
  as inert orphans: still retrievable by exact key via `get`, no longer
  enumerated by any listing. `purge` is the way to reclaim the space.
* The `standards` and `projects` namespaces, which remain writable via
  `save_to_recall` and `update_in_recall`.
* The pre-existing `!!null` encryption-key YAML defect tracked by
  [0001](0001-MADR-encryptionkey-yaml-tag-round-trip.md).

## Release

This is a user-visible breaking change: two CLI commands, one MCP tool, and a
block of configuration are removed. It warrants a minor bump from v1.1.1 with an
explicit removal note and an upgrade line stating that stale `harvest` config
entries are ignored and previously harvested records are retained read-only.
