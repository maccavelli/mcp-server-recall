---
status: accepted
date: 2026-11-17
---

# Adopt the fleet agent workspace and move records 0001-0008 under docs/decisions

## Context and Problem Statement

This repository is an existing product with no agent workspace. Source, a Makefile, eight MADR/PLAN pairs, and nine guides are present. `CLAUDE.md`, `AGENTS.md`, `.gitattributes`, `opencode.json`, and the five harness pointer trees are absent. `scripts/check_records.py` is absent, so the next record number is 0009 from a whole-repository scan of `NNNN-MADR-` and `NNNN-PLAN-` filenames. The eight pairs sit directly in `docs/`, which the documentation layout forbids: `docs/` holds `README.md`, `architecture.md`, and the `decisions/`, `reports/`, and `guides/` directories, and a record never sits directly in `docs/`. There is one product and no adjacent stack, so there is one documentation tree.

The Markdown lint config already in the tree is an older human-only file. It excludes MADR files and has no record profile. The fleet template's record profile requires headings these records do not have. A scratch copy of the sixteen files, linted with markdownlint-cli2 0.23.2 against that template and a legacy filter of `**/000[1-8]-{MADR,PLAN}-*.md`, reported 0 issues. The same copy with a filter that matched nothing exited 1: MD043 on every file (MADRs lack an H2 `## Consequences`; `0004-MADR-installer-runs-configure.md` also lacks `## Decision Drivers`; every PLAN lacks `## Grounding Reference`), plus MD040, MD010, and MD029 on some files. Those extra rules are exactly the ones the legacy override turns off. Rewriting the historical records to satisfy the full profile would change rationale and fenced blocks.

The repository hooks path resolves to the fleet global hooks directory. This record does not choose a commit, and it does not change product behavior. Self-update, installer generation, and CI alignment are a later decision.

## Decision Drivers

* One instruction file, discovered under both names, with a pointer for every harness.
* Records live under `docs/decisions/`. A record directly in `docs/` fails placement.
* The record checker fails on missing frontmatter, an unknown status, and a relative link that does not resolve.
* Historical rationale, fenced blocks, and record numbers stay. The legacy lint filter exists so adoption does not rewrite them.
* This change adds no dependency, no CI job, and no product behavior.

## Considered Options

* Adopt the fleet workspace, move records 0001-0008 under docs/decisions, and lint them on the legacy profile
* Add the instruction file and pointers and leave records 0001-0008 in docs/
* Rewrite records 0001-0008 onto the full heading profile in this change
* Leave the repository without an agent workspace

## Pros and Cons of the Options

### Adopt the fleet workspace, move records 0001-0008 under docs/decisions, and lint them on the legacy profile

* Good, because one `CLAUDE.md` plus an `AGENTS.md` symlink is the instruction file, and all five harnesses get both pointers.
* Good, because `git mv` keeps history, same-directory record links keep working, and the checker then enforces placement, frontmatter, status, and links.
* Good, because the legacy filter matches the sixteen records that exist today and does not match 0009, so this pair is held to the full profile.
* Neutral, because live links that walked up from `docs/` need one extra `../` after the move, and three cross-repository links have to become citations.
* Bad, because the first adoption replaces the existing Markdown lint config, so the human rules on guides and the root README become the fleet rules.

### Add the instruction file and pointers and leave records 0001-0008 in docs/

* Good, because agents would gain an instruction file without touching historical prose.
* Bad, because the checker reports each of those sixteen files as a record outside `docs/decisions/`.
* Bad, because the documentation layout would still be violated on the day it is adopted.

### Rewrite records 0001-0008 onto the full heading profile in this change

* Good, because one lint profile would cover every record.
* Bad, because the scratch lint shows the failures are missing required headings, untagged fences, hard tabs, and list prefixes. Several of those sit inside fenced blocks, which adoption does not edit.
* Bad, because retitling `### Consequences` to an H2, and inserting sections the originals never had, rewrites the historical shape of accepted decisions.

### Leave the repository without an agent workspace

* Good, because nothing in the product changes.
* Bad, because there is still no instruction file, no record gate, and the eight pairs remain directly in `docs/`.

## Decision Outcome

Chosen option: "Adopt the fleet workspace, move records 0001-0008 under docs/decisions, and lint them on the legacy profile", because it is the only option that installs the workspace, satisfies placement, and leaves historical rationale in place. The legacy filter is `**/000[1-8]-{MADR,PLAN}-*.md`. Record 0009 is outside that filter.

Frontmatter is repaired only where the checker rejects the file. Plans `0001` through `0004` have no opening frontmatter. Their date is 2026-08-29: that is the date in the body of the first two, and `git log` shows the same commit date for the other two and for their MADRs. Status is `accepted` for `0001`, `0003`, and `0004`. Plan 0001 still records a blocked live-host step, and plans 0003 and 0004 have no execution record, so `implemented` would claim more than those files say. Plan 0002 says all six phases completed on 2026-08-29, so its status is `implemented`. Plan 0007's frontmatter value `complete` is the writer-skill word and is not in the checker's PLAN vocabulary. The scratch checker reported: `'complete' is not a PLAN status` and listed `implemented` among the allowed values. The frontmatter value becomes `implemented`. Sentences in that plan's execution record that say it was closed as `complete` stay.

Three relative links point at `mcplib`'s `0002-MADR-xdg-compliant-user-paths.md` (`0001-PLAN-encryptionkey-yaml-tag-round-trip.md`, and two sites in `0002-MADR-configure-os-native-datastore-init.md`). A relative link cannot reach another repository. They become citations that name `mcplib` and that filename, with no URL. Nineteen in-repository links in the same two MADRs use `../` from `docs/` and break once the files sit in `docs/decisions/`. Each gains one `../`. Two guide links, in `docs/guides/repository-assessment.md` and `docs/guides/operations-and-security.md`, gain `decisions/` in the path. Same-directory links between records stay.

Go comments that mention `docs/0001-MADR-encryptionkey-yaml-tag-round-trip.md` are product source. They are out of this change. Prose inside historical records that mentions a `docs/000N-` path stays; it describes where the file lived when it was written.

## Consequences

* Good, because every later mutating change has an instruction file, a next-number command, and two gates: `make check-records` and `make markdownlint`.
* Good, because copied templates stay byte-identical except the named deltas: the `.gitignore` merge, the three `CLAUDE.md` slots, the two `docs/architecture.md` slots, the legacy lint filter, and the two sentences that would otherwise say no guide exists yet.
* Neutral, because `docs/README.md` is hand-kept. The checker has no index-writing flag, and the docs bootstrap says not to add one.
* Bad, because comments in `internal/config/config.go`, `cmd/mcp-server-recall/configure.go`, and `cmd/mcp-server-recall/configure_test.go` will name the old `docs/0001-MADR-encryptionkey-yaml-tag-round-trip.md` path until a later change.
* Bad, because the first commit that contains this workspace must not also contain product source, tests, CI, or a dependency change. The Go comment paths wait.

## Confirmation

The paired plan is the confirmation. A scratch copy, not the working tree, already showed the unfixed move fail: checker exit 1, four missing PLAN frontmatters, the illegal `complete` status, and 22 broken relative links. A planted guide link to a missing record failed a throwaway resolver. The pre-move guide path `docs/0005-MADR-remove-harvest-and-go-toolchain-dependency.md` does not exist once that file is under `docs/decisions/`; the retargeted path does. After execution, `python3 scripts/check_records.py --check-all` and `npx --yes markdownlint-cli2@0.23.2` exit 0, copied files match their templates aside from the named deltas, and `AGENTS.md` is a relative symlink to `CLAUDE.md`.

## More Information

The owner has not named a dependency policy or a pre-add policy in this turn. The dependency slot stays the template default. The pre-add slot names the command that already exists, `scripts/go-precheck.sh`, and the two Make gates this plan adds. It does not add a `preflight` target or any Go gate to the Makefile.

The root README already has an "I want to" table and a documentation-library list. Execution adds a link to `docs/README.md` and does not replace that README.

Applying the scaffold, moving the sixteen records, and editing guides are the paired plan. They wait for an explicit approval.
