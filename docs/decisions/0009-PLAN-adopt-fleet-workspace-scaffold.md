---
status: implemented
date: 2026-11-17
associated-madr: "0009-MADR-adopt-fleet-workspace-scaffold.md"
---

# Implement the fleet agent workspace for mcp-server-recall

Associated MADR: [0009-MADR-adopt-fleet-workspace-scaffold.md](0009-MADR-adopt-fleet-workspace-scaffold.md)

## Grounding Reference

* [0009-MADR-adopt-fleet-workspace-scaffold.md](0009-MADR-adopt-fleet-workspace-scaffold.md) decides the move, the legacy filter, and the frontmatter repairs.
* Skills loaded for this pair: `workspace-scaffolding`, `documentation-writing`, `madr-and-plan-writing`. Names were read from each skill's `name:` line.
* The record checker that will be copied has no `--write-index` flag. Statuses it accepts for a PLAN are `proposed`, `accepted`, `in-progress`, `deferred`, `implemented`, `rejected`, `withdrawn`, or `superseded by` an existing PLAN filename. It rejects `complete`.
* A scratch copy of the sixteen records moved to `docs/decisions/` with links unchanged failed that checker: exit 1, four missing frontmatters, one illegal status, 22 broken relative links, no broken anchors.
* markdownlint-cli2 0.23.2 on that copy: legacy filter `**/000[1-8]-{MADR,PLAN}-*.md` exited 0 with 0 issues; a filter that matched nothing exited 1.

## Goal

* The repository has one instruction file, `CLAUDE.md`, and `AGENTS.md` is a relative symlink to it.
* All five harnesses have both the MADR pointer and the coordinator pointer.
* Records 0001-0009 live in `docs/decisions/`. `docs/` no longer holds a record directly.
* `make check-records` and `make markdownlint` exist beside the current Go targets and exit 0.
* Historical record prose, fenced blocks, and numbers are unchanged except the link and frontmatter repairs this plan names.

## Scope

In scope: `.gitattributes`, the `.gitignore` merge, the Makefile splice, `scripts/check_records.py`, the Markdown lint config, `git mv` of the sixteen existing records, frontmatter on plans 0001-0004, the 0007-PLAN status value, the live links the scratch checker and the guide resolver found, `docs/README.md`, `docs/architecture.md`, a link from the root README, `CLAUDE.md`, the `AGENTS.md` symlink, and the pointer files in the workspace file set.

Out of scope, even in a later stage of this plan:

* Product source, tests, CI workflows, `go.mod`, and the installer scripts.
* Go comments that cite `docs/0001-MADR-encryptionkey-yaml-tag-round-trip.md` in `internal/config/config.go`, `cmd/mcp-server-recall/configure.go`, and `cmd/mcp-server-recall/configure_test.go`.
* Fenced blocks in records 0001-0008, including the comment example in `0001-PLAN-encryptionkey-yaml-tag-round-trip.md`.
* Prose path mentions such as `docs/0005-MADR-*` inside historical records.
* An empty `docs/reports/` directory.
* `.editorconfig`, Dependabot, `SECURITY.md`, `CODEOWNERS`, `CONTRIBUTING.md`, `CHANGELOG.md`, a local `user.name`, permission allowlists, a regular-file `AGENTS.md`, and a CI workflow.
* Self-update, generated installers, and CI alignment. Those need their own pair.
* Any commit, unless the owner asks for one in the execution turn. A commit then uses `git commit --no-edit` and no message flag. The hooks path already resolves to the fleet global hooks directory.

## Prerequisites

* The owner has approved this plan with `proceed`, `execute the plan`, or `do stage N`.
* The only intended dirty files before stage 1 are this pair. `main` matched `origin/main` when the pair was written.
* Do not renumber 0001-0008. Do not put 0009 into the legacy filter.

## Stage 1 — Line endings, ignores, and Make

1. Copy `templates/.gitattributes` to `.gitattributes`. Leave it byte-identical. Do not add a provenance comment.
2. Append the two fragment entries that `.gitignore` does not already have: `__pycache__/` and `*.py[cod]`. Do not duplicate `coverage_*.out`, `*.log`, `*.tmp`, `tmp/`, `temp/`, `.cache/`, or `.worktrees/`. Keep every existing line.
3. Prepend `templates/Makefile.snippet` to the existing Makefile. Keep `MOD_VERSION`, the version and `BUILD_KIND` stamps, and every current target. Do not add a `preflight` target or any Go test gate. Two `.PHONY` lines are acceptable.

## Stage 2 — Records move and docs gates

1. Copy `templates/scripts/check_records.py` to `scripts/check_records.py`.
2. Replace `.markdownlint-cli2.jsonc` with the template. The only edit is the legacy override's empty filter, set to `["**/000[1-8]-{MADR,PLAN}-*.md"]`.
3. `git mv` all sixteen `docs/000[1-8]-{MADR,PLAN}-*.md` files into `docs/decisions/` in one step, so no pair is split. Do not edit their prose in this stage.
4. Do not create `docs/reports/`.

## Stage 3 — Frontmatter

Add an opening frontmatter block to the four plans that lack one. Do not change any other line.

`0001-PLAN-encryptionkey-yaml-tag-round-trip.md`, date 2026-08-29, status `accepted`:

```yaml
---
status: accepted
date: 2026-08-29
associated-madr: "0001-MADR-encryptionkey-yaml-tag-round-trip.md"
---
```

`0002-PLAN-configure-os-native-datastore-init.md`, date 2026-08-29, status `implemented`, because the body says all six phases completed that day:

```yaml
---
status: implemented
date: 2026-08-29
associated-madr: "0002-MADR-configure-os-native-datastore-init.md"
---
```

`0003-PLAN-curl-bootstrap-installer.md` and `0004-PLAN-installer-runs-configure.md`, date 2026-08-29, status `accepted`. Use each plan's own MADR filename as `associated-madr`.

In `0007-PLAN-move-toolchain-to-go-1-27-4-and-floor-to-1-26-11.md`, change the frontmatter value `complete` to `implemented`. Leave the date, the `associated-madr` line, and every body sentence that says the plan was closed as `complete`.

## Stage 4 — Live links

Repair from checker output on a scratch copy of the moved tree before editing the working tree, then apply only the repairs that output lists. The measured set is:

1. Nineteen in-repository links in `0001-MADR-encryptionkey-yaml-tag-round-trip.md` and `0002-MADR-configure-os-native-datastore-init.md` start with `../` and point at `cmd/`, `internal/`, or `README.md`. After the move each needs one extra `../`. Link text stays. Same-directory record links stay.
2. Remove the URL from the three links whose target is `../../mcplib/docs/0002-MADR-xdg-compliant-user-paths.md` (one in `0001-PLAN-encryptionkey-yaml-tag-round-trip.md`, two in `0002-MADR-configure-os-native-datastore-init.md`). Keep a citation that names the `mcplib` repository and `0002-MADR-xdg-compliant-user-paths.md`.
3. In `docs/guides/repository-assessment.md`, change the target `../0005-MADR-remove-harvest-and-go-toolchain-dependency.md` to `../decisions/0005-MADR-remove-harvest-and-go-toolchain-dependency.md`.
4. In `docs/guides/operations-and-security.md`, change the target `../0006-MADR-domain-scoped-secondary-indexes.md` to `../decisions/0006-MADR-domain-scoped-secondary-indexes.md`.
5. Do not change `docs/guides/platform-installation.md`'s link to `../../go.mod`. A scratch tree without `go.mod` reported it missing; the repository has that file, and the guide does not move.

Run `--check-all` again. Fix only new `broken relative link` or `broken anchor` lines that this move caused. Do not edit a fenced block.

## Stage 5 — Docs index, architecture, and the root link

1. Write `docs/README.md` from the template. Fill every brace token. The index is hand-kept: one row per file for 0001-0009, status taken from frontmatter after stage 3, title taken from each file's H1. Status values to use: the eight existing MADRs stay `accepted`; plans 0001, 0003, 0004, 0005, and 0006 are `accepted`; plan 0002 and plan 0007 are `implemented`; plan 0008 stays `in-progress`; this pair stays `proposed` until the owner accepts the decision.
2. Replace the template sentence that says no guide exists. `docs/guides/` already holds nine guides. The "I want to…" table points at those guides plus `architecture.md`, `../CLAUDE.md`, and this MADR. Phrase rows as tasks. Use the root README's existing tasks: install, get a server running, connect a client, configure, use the CLI, call the MCP tools, read the dashboard, back up and understand security boundaries, and see what is complete or misleading.
3. Write `docs/architecture.md` from the template. Fill the what-it-is slot with: this repository is the `mcp-server-recall` server; BadgerDB is the source of truth; Bleve provides BM25 content search; fuzzy matching covers record keys; one documentation tree covers that product. Fill the what-is-not-here slot with: the datastore, its encryption key, and telemetry snapshots live on the host, not in this repository; release binaries, the installer, and self-update are product behavior decided in `0003-MADR-curl-bootstrap-installer.md`, `0004-MADR-installer-runs-configure.md`, and `0008-MADR-stamp-tag-builds-as-release-builds-and-fix-checksum-snippet.md`. Keep the fixed bullets above that slot.
4. In that architecture file's tree fence, replace the guides line that says the directory is created with the first guide. The directory already holds the guide library. This is a named delta because the template sentence would be false. Do not add coordinator paths to the fence; those files are installed in stage 6 and the template fence does not list them.
5. In the root README, add a link to `docs/README.md` from the documentation-library section. Do not replace the product introduction, the install instructions, or the existing "I want to" table. One new row in that table, pointing at `docs/README.md`, is enough.

## Stage 6 — Agent files

1. Write `CLAUDE.md` from the template. Fill the three slots and leave no brace token.
   * Scope: `mcp-server-recall` is a local MCP server for durable agent memory, structured records, file ingestion, full-text search, and terminal observability. What exists today is in `docs/architecture.md`. The record index is in `docs/README.md`.
   * Dependencies: no dependency may be added without a MADR in this repository that names it. Lockfiles change in the same commit as the first import, and are cleaned in the commit that removes the last. The first stack record owns the exact commands.
   * Pre-add: before staging, run `scripts/go-precheck.sh` on the changed Go files. Before every commit, run `make check-records` and `make markdownlint`. A file that fails is not committed.
2. From the repository root, `ln -s CLAUDE.md AGENTS.md`. The link target is the relative name `CLAUDE.md`.
3. Copy every file in the workspace file set's byte-for-byte table, including both coordinator pointers and both gitignores, and `opencode.json`. `cmp` each one against its template.

## Rollback

If these stages are applied and not pushed: delete the untracked files this plan added (`.gitattributes`, `scripts/check_records.py`, `CLAUDE.md`, `AGENTS.md`, `opencode.json`, `docs/README.md`, `docs/architecture.md`, and the pointer trees), `git mv` the sixteen records back to `docs/`, and restore `.gitignore`, `.markdownlint-cli2.jsonc`, the Makefile, the two guides, and the root README from git. Leave this pair in `docs/decisions/` unless the owner also asks to remove the proposal. Do not use `git reset --hard` or `git clean`. A pushed commit is not rewritten unless the owner asks.

## Completion Criteria

1. `python3 scripts/check_records.py --next` prints `0010`.
2. `python3 scripts/check_records.py --check-all` exits 0.
3. `npx --yes markdownlint-cli2@0.23.2` exits 0, and `make check-records` and `make markdownlint` exit 0.
4. `git ls-files -s CLAUDE.md AGENTS.md` shows mode `100644` for `CLAUDE.md` and mode `120000` for `AGENTS.md`, and the link reads `CLAUDE.md`.
5. `git ls-files --eol` shows `attr/text=auto eol=lf` on every tracked text file this change adds. Read the whole listing before accepting it.
6. `cmp` succeeds for every byte-for-byte file. The only diffs against templates are the named deltas in the MADR.
7. An identifier scan of the added and changed files finds no local account name, hostname, mail address, or home-path prefix. Describe the scan in the execution record without quoting the values.
8. The new gates are shown to fail on a planted input in a scratch copy: a record outside `docs/decisions/`, a PLAN with status `complete`, a relative link to a missing file, and a guide link to the pre-move path. The working tree is not the plant.

## Execution record

2026-11-17. The owner accepted the MADR and approved this plan. Every stage ran.

1. Stage 1 copied `.gitattributes` byte-identical, appended `__pycache__/` and `*.py[cod]` to `.gitignore`, and prepended the Makefile snippet. Existing Go targets stayed.
2. Stage 2 copied `scripts/check_records.py` byte-identical, replaced the Markdown lint config, and set the legacy filter to records 0001-0008. `git mv` moved the sixteen records into `docs/decisions/` together. `docs/` no longer holds a record directly. `docs/reports/` was not created.
3. Stage 3 added frontmatter to plans 0001-0004 and changed plan 0007's frontmatter status from `complete` to `implemented`. The two body sentences that say that plan was closed as `complete` stayed.
4. Stage 4 followed the checker. Before the repairs, `--check-all` exited 1 with 29 errors: four missing frontmatters, the illegal status, 22 broken record links, and the two guide links. After the repairs it exited 0. The nineteen in-repository links gained one `../`. The three `mcplib` links became citations. The two guide links gained `decisions/`. `docs/guides/platform-installation.md` still links to `../../go.mod`.
5. Stage 5 wrote `docs/README.md` and `docs/architecture.md`, including the guides-line delta, and added the documentation-index link and one row to the root README.
6. Stage 6 wrote `CLAUDE.md` with the three slots filled, linked `AGENTS.md` to `CLAUDE.md`, and copied the pointer set. `cmp` matched every byte-for-byte file.

Verification on the target: `python3 scripts/check_records.py --next` printed `0010`. `--check-all` exited 0. `npx --yes markdownlint-cli2@0.23.2` exited 0 (42 files, 0 issues). `make check-records` and `make markdownlint` exited 0.

Scratch plants, not the working tree: a control pair passed. A record at `docs/0099-MADR-outside.md` failed with `record outside its directory`. A PLAN with status `complete` failed with `is not a PLAN status`. A README link to `missing.md` failed with `broken relative link`. A guide link to `../0005-MADR-remove-harvest-and-go-toolchain-dependency.md` failed with `broken relative link`.

The identifier scan covered the local account name, the local mail address, the hostname, and home-path prefixes, read at run time. None of those values appear in the added or changed files. Matches on the generic prefixes `/home/` and `/Users/` are the fleet placeholder in `CLAUDE.md` and existing examples that already used `<user>` or the placeholder name `alex`.

This plan's status is `implemented` because the completion criteria passed. The checker rejects `complete`. No deviation from the approved stages. Go comments that cite the old `docs/0001-MADR-encryptionkey-yaml-tag-round-trip.md` path were left in place, as the plan requires.
