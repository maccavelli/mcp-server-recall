---
status: in-progress
date: 2026-11-17
associated-madr: "0007-MADR-move-toolchain-to-go-1-27-4-and-floor-to-1-26-11.md"
---

# 0007-PLAN: Implement Go 1.27.4 as the toolchain, `go 1.26.11` as the floor, golangci-lint v2.15.0 and `klauspost/compress` v1.20.2

Associated MADR:
[0007-MADR-move-toolchain-to-go-1-27-4-and-floor-to-1-26-11.md](0007-MADR-move-toolchain-to-go-1-27-4-and-floor-to-1-26-11.md)
(option A, as the owner ruled). Short form of go-selfupdate-lib's 0045-PLAN.

## Goal

* `go.mod` says `go 1.26.11` and `toolchain go1.27.4`; every current
  statement agrees.
* CI's lint step installs golangci-lint v2.15.0.
* `govulncheck ./...` exits 0 under Go 1.27.4 and Go 1.26.11.
* The plan stops before any tag.

## Scope

### Changes (line numbers at `8e8d31e`)

| File | Line | Change |
| :--- | :--- | :--- |
| `go.mod` | 3 | `go 1.26.6` becomes `go 1.26.11` |
| `go.mod` | after 3 | new line `toolchain go1.27.4` |
| `go.mod` | 11 | `klauspost/compress v1.18.6` becomes `v1.20.2` |
| `go.sum` | 150-151 | the two `v1.18.6` lines become the two `v1.20.2` lines |
| `Makefile` | 1 | `MOD_VERSION := 1.26.11` (declared, never used) |
| `.github/workflows/ci.yml` | 94 | `golangci-lint@v2.13.1` becomes `@v2.15.0` |
| `README.md` | 146-147 | "1.26.5." becomes "1.26.11 or later and is built and tested with Go 1.27.4." |
| `docs/guides/getting-started.md` | 11 | "Go 1.26.5" becomes "Go 1.26.11 or later (the repository is built and tested with Go 1.27.4)" |
| `docs/0005-MADR-*.md` | end | dated note A1 (no rewrite of history) |

### Stays as history

`docs/guides/repository-assessment.md:19` (a measurement),
`docs/0002-*` (Go 1.26.5 as the host's then toolchain), 0005's text.

### Out of scope

A floor CI job, a `govulncheck` step in CI, the tag and release, any
host install, any push (the coordinator commits and pushes).

## Rules

* Edits by a script under the scratchpad that asserts each old string's
  count; results read back. No heredocs.
* Heavy runs through `run-heavy`, one at a time. Exit status captured
  before any filter.
* The dependency bump was established on a scratch copy first (MADR E5);
  it moved one `go.mod` line and two `go.sum` lines, so it proceeds.
* Any lint finding is reported, not fixed or suppressed.

## Implementation Steps

1. **Red, before the change:** `govulncheck ./...` under `go1.27.4` and
   `go1.26.11`, `GOPROXY=off`: exit 3, GO-2026-6865 (MADR E4).
2. **Apply** `apply.py` (the table above) and `go get
   github.com/klauspost/compress@v1.20.2 && go mod tidy` on the tree (the
   module is in the cache after the scratch run; the proxy is not used
   again).
3. **Check** `go mod tidy -diff` is clean, `git diff --stat` names only
   the files above.
4. **Version-agreement check** (scratchpad script, not committed) passes
   on the tree and fails on three planted copies.
5. **Lint** with v2.15.0 built into a scratch `GOBIN`, under Go 1.27.4
   and `GOTOOLCHAIN=go1.26.11`.
6. **Gates:** fmt check, vet, tests (`run-heavy`), floor build/vet/test
   under `GOTOOLCHAIN=go1.26.11 GOPROXY=off`, `govulncheck` under both
   toolchains, linux and windows cross-builds with `CGO_ENABLED=0`,
   `actionlint`, markdownlint and the whitespace check.
7. **Stage** the two record files and the 0005 note only.

## Verification

* V1 red: exit 3 and 3 before.
* V2 green: `govulncheck` exit 0 and 0 after.
* V3 `go.mod`/`go.sum` diff is exactly the lines above.
* V4 version agreement passes and fails when planted.
* V5 lint exits 0 or its findings are reported.
* V6 tests pass under 1.27.4 and under 1.26.11.

## Rollout and Rollback

Nothing is installed or restarted. Rollback is `git revert` of the
change commit (the records stay). A tag, when the owner gives the word,
is `v2.1.1` from `main` after CI is green; an installed binary takes the
fix only from that release.

## Execution Record

All on 2026-11-17, at HEAD `8e8d31e` plus this change, `GOPROXY=off`
except where stated. Each exit status was taken before any filter.

### Scratch work (no tree writes)

* **Dependency, on a scratch copy of HEAD:** `go get
  github.com/klauspost/compress@v1.20.2` then `go mod tidy` moved
  `go.mod` line 11 and two `go.sum` lines only (MADR E5). The module proxy
  was used for that one download, on the scratch copy.
* **Linter:** v2.15.0 built with Go 1.27.4 into a scratch `GOBIN` (proxy
  used for that download); its `version` line: "golangci-lint has
  version 2.15.0 built with go1.27.4". v2.13.1 built from the module
  cache with Go 1.26.6: "golangci-lint has version 2.13.1 built with
  go1.26.6". On a scratch copy of HEAD, `make lint`: v2.13.1 under 1.26.6
  exit 0 "0 issues."; v2.15.0 under 1.27.4 exit 0 "0 issues."; v2.15.0
  under 1.26.6 exit 0 "0 issues.".

### Measured before and after

| Check | Before (HEAD) | After (this change) |
| :--- | :--- | :--- |
| `govulncheck ./...`, `go1.27.4` | exit 3, GO-2026-6865, `compress@v1.18.6`, fixed v1.20.2 | exit 0, "Your code is affected by 0 vulnerabilities." |
| `govulncheck ./...`, `go1.26.11` | exit 3, same | exit 0, same text |

After the change the scan still prints "2 vulnerabilities in packages you
import and 15 vulnerabilities in modules you require, but your code
doesn't appear to call these" (3 and 15 before). Those are not reached;
they are listed, not acted on.

### Gates (this Mac; heavy runs through `run-heavy`, which queued them)

| Gate | Start | End | Exit |
| :--- | :--- | :--- | :--- |
| `gofmt -l cmd internal` empty (CI's check) | 13:53:23 | 13:53:23 | 0 |
| `go vet ./...` (1.27.4) | 13:53:23 | 13:53:25 | 0 |
| `make lint` v2.15.0, `go1.27.4`: "0 issues." | 13:53:25 | 13:53:35 | 0 |
| `make lint` v2.15.0, `go1.26.11`: "0 issues." | 13:53:35 | 13:53:45 | 0 |
| `govulncheck`, `go1.27.4` | 13:53:45 | 13:53:50 | 0 |
| `govulncheck`, `go1.26.11` | 13:53:50 | 13:53:57 | 0 |
| cross-build `GOOS=linux`, `CGO_ENABLED=0`, netgo | 13:53:57 | 13:54:05 | 0 |
| cross-build `GOOS=windows`, `CGO_ENABLED=0` | 13:54:05 | 13:54:12 | 0 |
| floor `go build ./...`, `GOTOOLCHAIN=go1.26.11` | 13:54:12 | 13:54:15 | 0 |
| floor `go vet ./...`, same | 13:54:15 | 13:54:16 | 0 |
| `CGO_ENABLED=0 go test -count=1 -timeout 20m ./...` (1.27.4), nine packages `ok` | 13:54:16 | 14:17:05 (54 s run, rest queue) | 0 |
| floor `go test -count=1 -timeout 20m ./...`, `GOTOOLCHAIN=go1.26.11`, nine packages `ok` | 14:17:05 | 14:28:12 (53 s run, rest queue) | 0 |
| `actionlint .github/workflows/ci.yml` | 14:28:12 | 14:28:12 | 0 |
| `go mod tidy -diff` | 14:28:12 | 14:28:12 | 0 |
| markdownlint on `README.md` and `getting-started.md` | - | - | 0 |
| `git diff --check` | - | - | 0 |

### Version-agreement check (scratchpad script, not committed)

Passes on the tree ("floor go 1.26.11 (go.mod:3), toolchain go1.27.4
(go.mod:5)"). On scratch copies of the changed tree with one planted
break each it exits 1: toolchain `go1.27.3` at `go.mod:5`; floor
`1.26.10` at `go.mod:3` (and `Makefile:1`); `docs/guides/getting-started.md:11`
saying Go 1.26.5; `.github/workflows/ci.yml:94` with `golangci-lint@v2.13.1`.
No `go test` or other gate was changed or skipped.

## Deviations

None.
