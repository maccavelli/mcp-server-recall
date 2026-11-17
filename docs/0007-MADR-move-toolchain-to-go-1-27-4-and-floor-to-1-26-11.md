---
status: "accepted"
date: 2026-11-17
decision-makers: maccavelli
consulted: "govulncheck v1.9.0 (vulnerability database updated 2026-11-11 02:51 UTC) under Go 1.27.4 and 1.26.11; golangci-lint v2.13.1 and v2.15.0 built and run on scratch copies; actions/setup-go's README at the pinned commit; go-selfupdate-lib's 0045-MADR and 0045-PLAN as the model"
informed: maccavelli
---

# 0007-MADR: Move the toolchain to Go 1.27.4 and the floor to `go 1.26.11`, lint with v2.15.0, and bump `klauspost/compress` for GO-2026-6865

## Context and Problem Statement

The owner's instruction of 2026-11-17, relayed by the coordinator, verbatim:

> "go-llmprovider-sdk, go-tui-lib, acp-go-sdk2, gobble-cli, magic-cli-remote, go-selfupdate-lib, mcp-server-duckduckgo, mcp-server-magictools, mcp-server-recall, mcp-server-sequential-thinking, mcplib, mcp-buntdb, and mcp-server-socratic-thinker all need to have their go floor pinned to 1.26.11, and go version/toolchain pinned to 1.27.4."

"Floor" is the `go` directive in `go.mod`; "version/toolchain" is the
`toolchain` line. This repository has `go 1.26.6` and no `toolchain` line
(`go.mod:3`).

The owner then ruled on a survey of the thirteen repositories. The labels
below are quoted exactly and only as labels; the text beside each says
what was asked.

* **D1, this repository's place.** Chosen label: "One short record pair
  per repository, all approved now through CI (Recommended)". Asked: seven
  repositories, this one among them, are ready for floor 1.26.11 and
  toolchain 1.27.4; one short pair each, modelled on go-selfupdate-lib's
  0045, executed (`go.mod`, docs, gates) and pushed to `main` on this
  approval, libraries first, then the servers. The coordinator stops and
  returns to the owner on any failure or anything unexpected. Tags and
  releases wait for the owner's word.
* **D2, the dependency.** Chosen label: "Yes: bump the dependency in the
  same record as the Go move (Recommended)". Asked: whether to bump
  `github.com/klauspost/compress` for GO-2026-6865 in the same record. The
  bump is established first on a scratch copy (which version fixes it,
  whether anything else moves in `go.sum`), and it stops if it pulls in
  more than that one dependency.
* **D3, the linter.** Chosen label: "v2.15.0 everywhere, in each
  repository's Go-pins record (Recommended)". Asked: golangci-lint
  v2.13.1 cannot type-check code under Go 1.27.4 ("export data version 5
  is greater than maximum supported version 4", seen in a sibling's CI);
  each record carries the CI pin, and any new finding the newer linter
  reports goes to the owner with the code rather than being suppressed.

Everything in this record other than these quotations is evidence or the
author's text.

Why now: GO-2026-6859, in `net/http`, is reported under Go 1.27.3 and
1.26.10 and not under 1.27.4 and 1.26.11 (measured 2026-11-17 with
`govulncheck` v1.9.0). The same tool run on this repository reports a
second, separate advisory, GO-2026-6865 in a dependency (E4).

**Evidence index.** Read or measured on 2026-11-17 at HEAD `8e8d31e`, on
branch `main`, tree clean, level with `origin/main`, unless stated.

* **E1, the pins [read].** `go.mod:3` `go 1.26.6`, no `toolchain` line.
  `Makefile:1` `MOD_VERSION := 1.26.6`: a grep for `MOD_VERSION` finds
  only that line, so the variable is declared and never used; it is moved
  with the floor so it does not state a stale number.
  `README.md:147` says "1.26.5." and
  `docs/guides/getting-started.md:11` says "Go 1.26.5": both were already
  stale against `go.mod` and are made true here.
  `.github/workflows/ci.yml:94` installs
  `golangci-lint@v2.13.1`. The Makefile's lint target
  (`Makefile:53-58`) reads `GOLANGCI_LINT`, defaulting to the binary in
  `GOPATH/bin`, with the config `.golangci.yml`.
* **E2, history that stays.** `docs/guides/repository-assessment.md:19`
  records `go version go1.26.5 darwin/arm64`, a measurement of that day.
  `docs/0002-MADR-*` and `docs/0002-PLAN-*` name Go 1.26.5 as what the
  host had then; they stay. `docs/0005-MADR-*` names `~/sdk/go1.26.5` as
  one of the places the removed harvest subsystem searched for a runtime
  `go` binary. 0005 is about the product needing no Go at run time; this
  record concerns the Go that builds the product, so 0005 is not
  affected and carries only the dated note below.
* **E3, CI [read].** `ci.yml:59`, `:198` and `:264` set up Go with
  `actions/setup-go` pinned at commit
  `b7ad1dad31e06c5925ef5d2fc7ad053ef454303e` (v7.0.0) and
  `go-version-file: go.mod`. That README at that commit, fetched
  2026-11-17 (line 25): "Supports both `go` and `toolchain` directives in
  `go.mod`. If the `toolchain` directive is present, its version is used;
  otherwise, the action falls back to the `go` directive." So CI builds
  with **1.27.4** after this change and with 1.26.6 today. **CI has no job
  that builds or tests on the floor**: every Go job reads `go.mod`. That
  is a named gap (see Consequences), not changed here. The release job
  calls `maccavelli/mcplib`'s reusable workflow pinned by commit
  (`ci.yml:343`); `publish-selfupdate-release.yml` at that commit has no
  `setup-go`, `go-version` or `toolchain` text (read 2026-11-17), so the
  release binaries are built by this repository's own jobs.
* **E4, `govulncheck ./...` before [measured, `GOPROXY=off`].**
  govulncheck v1.9.0, database updated 2026-11-11 02:51:11 UTC:

  | Toolchain | Exit | Result |
  | :--- | :--- | :--- |
  | `go1.27.4` | 3 | GO-2026-6865, `github.com/klauspost/compress@v1.18.6`, fixed in v1.20.2 |
  | `go1.26.11` | 3 | the same |

  Neither toolchain reports a standard-library advisory, so the move to
  1.27.4 and 1.26.11 closes GO-2026-6859 for builds and the dependency
  advisory is what remains.
* **E5, the dependency, on a scratch copy of HEAD [measured].**
  `klauspost/compress` is a **direct** requirement (`go.mod:11`, no
  `// indirect`): `go mod why -m` shows `internal/memory` importing
  `github.com/klauspost/compress/zstd`. `badger/v4@v4.9.1` also requires
  it, at `v1.18.0` (`go mod graph`). `go get
  github.com/klauspost/compress@v1.20.2` followed by `go mod tidy`
  succeeded and changed exactly one line of `go.mod` (line 11,
  `v1.18.6` to `v1.20.2`) and two lines of `go.sum`
  (the `h1:` and `/go.mod h1:` lines of the old version replaced by those
  of v1.20.2). No other requirement and no new module moved. **The
  module proxy was used for that one download, on the scratch copy
  only**, as the owner's rule allowed.
* **E6, the linter [measured].** golangci-lint v2.15.0 was built with Go
  1.27.4 into a scratch `GOBIN` (the module proxy was used for that one
  download), and v2.13.1 was built from the module cache with Go 1.26.6
  (cache only). On a scratch copy of HEAD, `make lint` with each
  (`GOLANGCI_LINT=<scratch binary>`): v2.13.1 under Go 1.26.6 exits 0,
  "0 issues."; v2.15.0 under Go 1.27.4 exits 0, "0 issues."; v2.15.0
  under Go 1.26.6 exits 0, "0 issues." So v2.15.0 reports **no new
  finding** at HEAD.
* **E7, who requires this module [read].** It is a server binary. Its
  `go.mod` module path is `github.com/maccavelli/mcp-server-recall`; a grep
  of the `go.mod` files one level under the fleet's Go directory finds no
  other module that requires it. The consumers are the
  installed binaries, delivered by the release workflow and the
  `curl | sh` installer (0003-MADR).

The question: how should this repository's builds and declared floor
follow the owner's rulings, and how should the one dependency advisory be
closed, with the fewest changes?

## Decision Drivers

* The owner's rulings D1 to D3.
* Ship nothing built on a toolchain or dependency with a published
  advisory that `govulncheck` reaches (E4).
* **The tree states one floor and one toolchain**; every current statement
  agrees with `go.mod`, and the already-stale 1.26.5 statements are made
  true.
* **No gate is loosened and no finding suppressed.**
* The dependency bump stops if it moves more than that one module (D2).
* No compatibility engineering: the owner is the only user of the fleet.

## Considered Options

* **A. Floor `go 1.26.11`, `toolchain go1.27.4`, linter v2.15.0 and
  `klauspost/compress` v1.20.2, in one change (as ruled).**
* **B. Toolchain 1.27.4 and linter only; floor stays `go 1.26.6`; the
  dependency waits.**
* **C. Change nothing.**

## Decision Outcome

Chosen option: **A**, because it is the owner's instruction and rulings
(D1 to D3), and the measurements show it costs one `go.mod` line, two
`go.sum` lines and no new finding.

1. **`go.mod`:** `go 1.26.6` becomes `go 1.26.11` (`:3`); a line
   `toolchain go1.27.4` is added after it; `klauspost/compress` `v1.18.6`
   becomes `v1.20.2` (`:11`). `go.sum` changes the two lines of E5.
2. **`Makefile:1`:** `MOD_VERSION := 1.26.11`.
3. **`ci.yml:94`:** `golangci-lint@v2.13.1` becomes `@v2.15.0`. The
   workflow changes nowhere else.
4. **Docs:** `README.md:147` and `docs/guides/getting-started.md:11` say
   Go 1.26.11 or later, built and tested with Go 1.27.4.
5. **History stays:** `repository-assessment.md:19`, 0002's Go 1.26.5
   statements, 0005's runtime-search path.
6. **0005-MADR** gains a dated note (A1) saying it concerns the runtime
   toolchain the product no longer needs and is unaffected by this
   record.

### Consequences

* Good, because CI's jobs build with Go 1.27.4 (E3) and `govulncheck`
  should report clean under both 1.27.4 and 1.26.11; the PLAN measures it.
* Good, because the linter can type-check Go 1.27.4 code in CI (D3).
* Bad, because **the floor rises by five patch releases** (1.26.6 to
  1.26.11). Only a build from source is affected (the product needs no Go
  at run time, 0005): `go` selects 1.26.11 or later by itself unless
  `GOTOOLCHAIN=local` is set on an older Go.
* Bad, because **CI has no floor job** (E3): nothing in CI builds or tests
  with Go 1.26.11, so the declared floor is proved by CI only through the
  `go` line's own constraint. The PLAN proves it locally on one host. A
  floor job, if wanted, is a separate decision for the owner, fleet-wide;
  it is not added here.
* Bad, because the dependency moves two minor versions (1.18.6 to 1.20.2)
  of a compression library the datastore uses on every record
  (`internal/memory`): the repository's whole test suite is the check, and
  data written by the old version is read by the new one only as far as
  the library keeps zstd's format stable (zstd is a fixed format; not
  tested here with a database written before the bump, see Not verified).
* **A release is needed for an installed binary to benefit.** Installed
  binaries embed the dependency and the standard library at the time they
  were built; the installer and self-update fetch release assets. Nothing
  here installs or restarts anything. The next tag, which would be
  `v2.1.1` (the latest is `v2.1.0`), builds with the new pins; **the tag
  and the release need the owner's word, and the PLAN stops before them.**

### Confirmation

* `govulncheck ./...` under `go1.27.4` and `go1.26.11`, `GOPROXY=off`,
  exits 0 (before: 3 and 3, E4).
* `go mod tidy -diff` shows nothing; `git diff go.mod go.sum` shows only
  the lines above.
* The PLAN's version-agreement check passes on the tree and fails on
  three planted wrong copies (toolchain, floor, a stray old pin including
  the lint pin).
* `make lint` with v2.15.0 under Go 1.27.4 and `GOTOOLCHAIN=go1.26.11`
  reports no finding; the repository's tests pass; the floor build, vet
  and tests pass under `GOTOOLCHAIN=go1.26.11`.
* CI on `main`, after the coordinator pushes, passes every job and its
  `setup-go` logs print `go1.27.4`.

## Pros and Cons of the Options

### A. The full move (chosen)

* Good: matches the rulings, closes both advisories, one linter version.
* Bad: the floor and the dependency move together, so a regression in
  either is found in one run, not separated.

### B. Toolchain and linter only

* Good: no dependency change.
* Bad: `govulncheck` keeps exiting 3 (E4) and the advisory stays in the
  shipped binary; it ignores D2 and the floor ruling.

### C. Change nothing

* Good: no risk of regressions.
* Bad: CI's `golangci-lint` v2.13.1 fails under the 1.27.4 toolchain the
  owner wants (D3), and the advisory stays.

## More Information

### Amendments

* **A1 (2026-11-17), 0005-MADR.** Its mention of `~/sdk/go1.26.5`
  describes the places the removed harvest code searched for a runtime
  `go` binary. It is unaffected by the 2026-11-17 move to Go 1.27.4 and
  `go 1.26.11`, which concerns the Go that builds the binary. See
  0007-MADR. (The note is added to 0005-MADR in the same commit.)

### Not verified

* That CI's `setup-go` installs 1.27.4 on the runners: the README says it
  follows the `toolchain` line; no run has been seen.
* CI and the Makefile run no `govulncheck` (a grep finds none), so the
  clean result is a local measurement with v1.9.0; no CI step checks it.
  Adding one is the owner's decision and is not made here.
* Reading a database written before the bump with the new library: not
  tested; the test suite builds fresh stores.
* Whether the vulnerable symbol's reachability in GO-2026-6865 affects
  this server's use in practice: `govulncheck` reports reachable; the
  advisory text was not analysed beyond its fixed version.
* The upstream's state beyond the local `origin/main` ref: no fetch was
  made. The repository has one remote, `origin`.

### Links

* [GO-2026-6865](https://pkg.go.dev/vuln/GO-2026-6865) and
  [GO-2026-6859](https://pkg.go.dev/vuln/GO-2026-6859).
* go-selfupdate-lib's 0045-MADR, the model for this record's form.
* [0007-PLAN](0007-PLAN-move-toolchain-to-go-1-27-4-and-floor-to-1-26-11.md).
