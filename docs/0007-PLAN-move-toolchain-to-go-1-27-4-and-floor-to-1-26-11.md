---
status: complete
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

## Amendment A1 (2026-11-17): release phase for `v2.1.1`

The pair and the change were committed and pushed to `main`
(`7c09b7c` the records, `e7f162b` the change). The owner then gave the
word for the release, as the coordinator relayed it, verbatim: "You may
push the mcp-server-recall v2.1.1 tag". The coordinator makes and pushes
the tag; this amendment only records what is checked before and after.
Nothing above is rewritten. Nothing here installs or updates anything on
a developer machine: every check is a read-only API query or a scratch
build.

### R0. CI on `e7f162b` before the tag (read-only)

Run "CI", push event, concluded `success`. The run on `7c09b7c` shows
`cancelled`: `ci.yml:15-17` sets `cancel-in-progress` to
`${{ github.ref_type != 'tag' }}`, so a newer push on a branch cancels an
older run, and a tag run is never cancelled.

| Job | Result | Evidence quoted from the run's log |
| :--- | :--- | :--- |
| Go (linux/amd64; build on tag) | success | "Setup go version spec 1.27.4", "Successfully set up Go version 1.27.4", "go version go1.27.4 linux/amd64"; `go install ...golangci-lint@v2.15.0` then `golangci-lint run -c .golangci.yml ./...` and "0 issues."; `CGO_ENABLED=0 go test ./...`: nine packages `ok` |
| Go (linux/arm64) | success | "Successfully set up Go version 1.27.4", "go version go1.27.4 linux/arm64"; nine packages `ok` |
| Go (windows/amd64) | success | "Successfully set up Go version 1.27.4"; eight packages `ok`, `internal/lifecycle` "[no test files]" |
| Go (darwin/arm64) | success | "Successfully set up Go version 1.27.4", "go version go1.27.4 darwin/arm64"; build, vet, test steps passed; the hosted macOS runner waited about nine minutes in the queue, then ran (9 steps) |
| Smoke, Publish Release assets, Publish self-update release | skipped | gated on a tag (`ci.yml:213`, `:251`, `:337`) |

So the `toolchain` line took effect in CI: `go-version-file: go.mod`
installed 1.27.4, as MADR E3 predicted from the action's README. The
v2.15.0 linter ran in CI with 0 issues. CI still has no floor job and no
`govulncheck` step (MADR consequences), so neither is shown by CI.

### R1. What a `v*` tag push runs (read from `ci.yml` and the pinned reusable workflow)

`on:` includes `tags: ["v*"]` (`ci.yml:6-7`); a tag run is never
cancelled (`:17`). Jobs, in order:

1. **`go`** (ubuntu-24.04, the `go` job's tag steps). `Build release
   binaries` (`:111-163`) requires the tag to match `^v[0-9]+\.[0-9]+\.[0-9]+$`,
   sets `VER=${TAG#v}` and runs `make build-all VERSION="$TAG"`. The
   version is stamped by a linker flag in the Makefile's targets:
   `-X main.RawVersion=$(VERSION) -X main.RawBuildKind=$(BUILD_KIND)`,
   with `RawBuildKind` set to "release" for a tag build. No version is
   hard-coded in Go source (`cmd/mcp-server-recall/version.go:12` defaults
   `RawVersion` to "dev"). It builds four binaries: linux/amd64,
   linux/arm64, darwin/arm64, windows/amd64 (`.exe`), all `CGO_ENABLED=0`
   (checked per binary), runs the linux/amd64 binary and requires
   `--version` to print the version, and writes one `SHA256SUMS`. It
   uploads them as a workflow artifact.
2. **`go-native`** (linux/arm64, macos-15, windows-2025): build, vet, test.
3. **`smoke-native`** (tag only; needs `go`): runs the linux/arm64,
   darwin/arm64 and windows/amd64 binaries and requires `--version` to
   equal the version.
4. **`release`** (tag only; needs `go`, `go-native`, `smoke-native`):
   re-verifies `SHA256SUMS`, re-checks cgo-free and the version, stages
   the seven files (the four binaries, `SHA256SUMS`, `install.sh`,
   `install.ps1`) and uploads them as artifact `recall-release-<tag>`. It
   creates no release itself.
5. **`publish`** (tag only; `ci.yml:335-349`) calls mcplib's reusable
   workflow, pinned by commit `8d4d89ff…` (mcplib v1.4.1), with
   `contents: write`, `id-token: write`, `attestations: write`. That
   workflow (read in full at that commit): requires a strict
   `vX.Y.Z` tag; **refuses if any release for the tag already exists**,
   drafts included; downloads the artifact; validates that the file set is
   exactly the four canonical binaries, `SHA256SUMS`, `install.sh` and
   `install.ps1`, with matching digests; `gh release create <tag> --draft
   --verify-tag --generate-notes` and uploads the files (never
   `--clobber`); **attests** the files with
   `actions/attest-build-provenance` (keyless, GitHub's OIDC identity: no
   signing key); publishes the draft with `gh release edit --draft=false`;
   then waits up to 120 s for the release to be immutable and
   `gh release verify <tag>` to pass.

Secrets: the only secret the workflow reads is the built-in
`GITHUB_TOKEN` (`ci.yml:275`; `grep secrets\.` finds no other). There is
no signing key. The release is created as a draft and published at once
inside the same job; it has no `--latest` or `--prerelease` flag, so it is
a normal release and, being the newest, becomes the repository's "latest".
The previous release `v2.1.0` shows the same shape: not a draft, not a
prerelease, immutable, and "Latest", with the seven assets
`install.ps1`, `install.sh`, `mcp-server-recall-darwin-arm64`,
`mcp-server-recall-linux-amd64`, `mcp-server-recall-linux-arm64`,
`mcp-server-recall-windows-amd64.exe`, `SHA256SUMS`.

**What pushing the tag changes for users.** `install.sh` and `install.ps1`
are fetched from `releases/latest/download/` (`scripts/install.sh:4`,
`:70`; `docs/guides/platform-installation.md:168`), and `mcp-server-recall
update` checks GitHub releases (`cmd/mcp-server-recall/update.go:97`) and,
without `--version`, installs "the latest release" (`README.md:15`). Once
`v2.1.1` is published as latest, every new install and every plain `update`
resolves to it (how the updater picks "latest" is in mcplib's code, not
read here). Existing installs change only when their owner runs `update`.
Releases are immutable, so a bad one cannot be edited, only superseded.

### R2. The repository's release procedure and the tag form

* **Procedure.** There is no releasing guide. The rules are in
  `README.md:34-36` (releases are immutable `vMAJOR.MINOR.PATCH` tags that
  publish the exact `mcp-server-recall-<goos>-<goarch>[.exe]` assets and one
  `SHA256SUMS`; a rebuilt fix gets a new patch tag) and in the workflow
  above. No record under `docs/` describes the steps that released
  `v2.1.0` or `v2.0.0`: a search of `docs/0001` to `docs/0006` for those
  versions finds none. The `v2.0.0` tag message names `0005-MADR-remove-harvest-and-go-toolchain-dependency.md`
  and `0006-MADR-domain-scoped-secondary-indexes.md`.
* **Tag form.** Annotated, unsigned (no signature block in the tag object;
  `tag.gpgsign` is unset). `v2.1.0` has the message `mcp-server-recall
  v2.1.0` and `v2.0.0` has `v2.0.0`; the latest is followed:
  `v2.1.1` with the message `mcp-server-recall v2.1.1`, on the commit
  CI passed (`e7f162b`, or a later commit that only changes records).
* **Before the tag:** nothing in the tree names the current version in a
  way the build reads: the version comes from the tag through the linker
  flag, and a grep for `2.1.0` in Go source, scripts and the Makefile
  finds nothing. No pre-tag docs change is required. `README.md:17`
  (`update --version v2.1.0 # install that exact release`) is an example of
  pinning an exact release, not a "latest" pointer; it stays true and is
  left alone.
* **After the tag:** the checks in R4.

### R3. What the tag carries

`git log v2.1.0..HEAD` lists three commits:

| Commit | Changes | Kind |
| :--- | :--- | :--- |
| `8e8d31e` docs: replace local account paths with placeholders | `docs/0002-MADR-*` only | docs |
| `7c09b7c` docs(adr): add 0007 record | the 0007 pair and the 0005 note | docs |
| `e7f162b` chore(deps): bump Go toolchain, golangci-lint, and klauspost/compress | `go.mod`, `go.sum`, `Makefile`, `ci.yml`, README, getting-started | dependency and build |

`git diff --stat v2.1.0..HEAD` touches no `.go` file. The shipped binary
differs only by the standard library of Go 1.27.4 (CI's toolchain) and
`klauspost/compress` v1.20.2. No feature and no breaking change: a PATCH
number is right.

### R4. Compatibility of the dependency bump for existing data

Established on scratch, with no real user data. Two tool builds were made
from `git archive` copies of the `v2.1.0` tag (compress v1.18.6) and of
`HEAD` (v1.20.2), each with the same small program added under
`internal/memory/` that calls the package's own `NewMemoryStore`, `Save`
and `Get`. The program takes an explicit store directory and refuses any
path outside the scratch store root (shown: a path elsewhere exits 2), and
the tool never calls the configuration's default data directory
(`internal/config/dirs.go:47`), so the real store is not touched. It
writes 300 records (four domains, compressible, random, unicode and short
bodies of up to about 22 KB), and the reader fetches all 300 by key and
prints key, domain, SHA-256 of the content, title and tags.

| Direction | Store | Result |
| :--- | :--- | :--- |
| `v2.1.0` writes, HEAD reads | no encryption key | 300 of 300 present, output byte-identical to the `v2.1.0` read |
| `v2.1.0` writes, HEAD reads | 32-byte encryption key | same |
| `v2.1.0` reads again after HEAD opened it | both | byte-identical (a rollback after an upgrade reads) |
| HEAD writes, `v2.1.0` reads (rollback) | both | 300 of 300, byte-identical to HEAD's read |

Seen to fail: reading an empty directory prints 300 MISSING lines and
`cmp` reports a difference. Not shown: a real user's data (sizes,
history, long-lived compaction state), the `serve` process, or `export` and
`import`; this is the storage API, with and without encryption. The
library's release notes were not read.

### R5. Steps

1. Owner's word: given (quoted above). The coordinator tags and pushes.
2. The tag points at a commit whose CI is green (R0). `git tag -a v2.1.1
   -m "mcp-server-recall v2.1.1" <commit>`; push the tag only.
3. After the push, read-only, none of it on a developer's installed binary:
   * the tag's "CI" run on `v2.1.1` concludes `success` with `go`,
     `go-native` (three), `smoke-native` (three), `release` and `publish`
     all green; the `go` job's summary reads "Stamped: `2.1.1` from tag
     `v2.1.1`" (`ci.yml:161`);
   * `gh release view v2.1.1 --json tagName,isDraft,isPrerelease,isImmutable,assets`:
     not a draft, not a prerelease, immutable, and exactly the seven asset
     names listed in R1;
   * `gh release verify v2.1.1` passes (release attestation), and for
     each asset `gh release verify-asset v2.1.1 <downloaded file>` where
     practicable;
   * the documented checksum check: download `SHA256SUMS` and each binary
     to a scratch directory (`docs/guides/platform-installation.md:167-172`)
     and compare; run no downloaded binary on a machine that has the
     product installed, except the linux/amd64 or the host's own build
     with `--version` in scratch, which must print `2.1.1`;
   * `gh api repos/<owner>/mcp-server-recall/releases/latest --jq .tag_name`
     prints `v2.1.1`, which is what `update` and the installers resolve;
   * `curl -fsSIL` on `releases/latest/download/SHA256SUMS` returns 200.
4. Close the plan: execution record, `status: complete`.

### R6. Rollback

A tag is never moved or deleted, and a published release is immutable.
Before the push, nothing exists to undo. After it, a fault is fixed by a
new patch release `v2.1.2`; users who updated can pin the previous
release with `update --version v2.1.0`, which is reported as an explicit
rollback (`README.md:25`), and the compatibility check R4 shows the
`v2.1.0` binary reads data HEAD wrote. A run of the publish job that fails
before "Publish the draft" leaves only a draft release; the workflow
refuses a second attempt on the same tag while a draft exists, so a draft
is removed by the owner in the repository's release page, and the tag is
not moved.

## Amendment A2 (2026-11-17): post-tag checks (R5) and a correction to R1

The coordinator made the annotated, unsigned tag `v2.1.1` (message
`mcp-server-recall v2.1.1`; tag object `c18d5c28…`, as `gh release verify`
resolved it) on `e7f162b` after the owner's word and a green CI run, and
pushed only the tag. These checks were read-only; nothing was installed,
and no downloaded binary was run.

### Tag CI (run 44173156024, event push, branch `v2.1.1`): `success`

* Jobs, all `success`: Go (linux/amd64; build on tag) 17 steps, Go
  (linux/arm64) 9, Go (windows/amd64) 9, Go (darwin/arm64) 9, Smoke
  (linux/arm64) 4, Smoke (darwin/arm64) 4, Smoke (windows/amd64) 4,
  Publish Release assets 9, Publish self-update release / publish 13. The
  hosted macOS native job waited in the queue (it began about 10 minutes
  after the others) and then ran all nine steps.
* Toolchain: every Go job logged "Successfully set up Go version 1.27.4".
* Build: the `go` job's `make build-all` lines show `-X
  main.RawVersion=v2.1.1` for all four targets; "ok:
  dist/mcp-server-recall-darwin-arm64 is cgo-free", and the same for
  linux-amd64, linux-arm64 and windows-amd64.exe; the artifact
  `mcp-server-recall-2.1.1` was uploaded.
* Smoke, each binary on its own platform: "ok:
  dist/mcp-server-recall-linux-arm64 2.1.1", "ok:
  dist/mcp-server-recall-darwin-arm64 2.1.1", "ok:
  dist/mcp-server-recall-windows-amd64.exe 2.1.1".
* Release job: the `SHA256SUMS` check printed
  "mcp-server-recall-darwin-arm64: OK", "-linux-amd64: OK",
  "-linux-arm64: OK", "-windows-amd64.exe: OK"; then "ok: linux-amd64
  2.1.1".
* Publish job: the strict-tag check, the refuse-existing-release script
  and "verify-selfupdate-release: ok" passed; `gh release create
  "v2.1.1" --draft --verify-tag --generate-notes`; "Attestation created for
  7 subjects"; `gh release edit "v2.1.1" --draft=false` (the release URL
  printed at 21:00:50Z); then the immutability wait loop, which runs `gh
  release verify`, ended the job in success.

### The release (read-only API)

* `gh release view v2.1.1`: `tagName` v2.1.1, `name` v2.1.1, `isDraft`
  false, `isPrerelease` false, `isImmutable` true, `publishedAt`
  2026-11-17T21:00:50Z, `targetCommitish` main.
* Assets, seven, the same names as `v2.1.0`: `install.ps1` (6633 bytes),
  `install.sh` (10567), `mcp-server-recall-darwin-arm64` (26425026),
  `mcp-server-recall-linux-amd64` (27570336),
  `mcp-server-recall-linux-arm64` (25559200),
  `mcp-server-recall-windows-amd64.exe` (28227072), `SHA256SUMS` (391).
* `gh api repos/<owner>/<repo>/releases/latest --jq .tag_name`: `v2.1.1`,
  so the installers' `releases/latest/download/` URLs resolve to it, and a
  plain `update` should.
* `gh release verify v2.1.1`: exit 0, "Resolved tag v2.1.1 to
  sha1:c18d5c28…", "Loaded attestation from GitHub API", "Release v2.1.1
  verified!", listing a sha256 for each of the seven assets.
* Checksums: `gh release download` of `SHA256SUMS` and the four binaries
  into a scratch directory, then `sha256sum -c SHA256SUMS`: all four `OK`
  (exit 0); the digests equal the ones `gh release verify` listed. `file`:
  darwin-arm64 "Mach-O 64-bit executable arm64"; linux-amd64 "ELF 64-bit
  LSB executable, x86-64 … statically linked"; linux-arm64 "ELF 64-bit LSB
  executable, ARM aarch64 … statically linked"; windows-amd64.exe "PE32+
  executable (console) x86-64, for MS Windows".

### Two findings, both older than this plan (not caused by it)

1. **The guide's checksum snippet matches nothing.**
   `docs/guides/platform-installation.md:170-172` picks the expected
   digest with an `awk` that needs a versioned name
   (`mcp-server-recall-linux-<arch>-<digit>…`). `SHA256SUMS` lists the
   exact basenames (`mcp-server-recall-linux-amd64`, …), as `v2.1.0`'s
   also does. Run on this release's file the snippet's `awk` prints
   nothing, so `test -n "$EXPECTED"` would fail. This covers the guide
   only; the installer scripts were not run.
2. **Release binaries are stamped as local builds, by the workflow's
   command.** Correction to R1: R1 said the build sets `RawBuildKind` to
   "release" for a tag. The tag run's own build lines show `-X
   main.RawBuildKind=local`, because `ci.yml:124` runs `make build-all
   VERSION="$TAG"` and passes no `BUILD_KIND`, while the Makefile's comment
   (`Makefile:6-9`) says the release workflow passes it. `v2.1.0`'s
   workflow has the same command. `update.go:78-84` treats anything but
   "release" as a local build, and `README.md:32-33` says `update` refuses
   to replace a local build without `--force`. Not tested by running a
   binary; this is read from the workflow log and the source. Whether it
   is a defect, and the fix, are for the owner; it is outside this plan
   and neither is touched here.

### Status

`status: complete`. Checked: V1 (red, before), V2 (`govulncheck` exit 0 and
0, after), V3 (the `go.mod` and `go.sum` lines), V4 (version agreement,
planted), V5 (lint 0 issues, locally and in CI), V6 (tests under 1.27.4,
1.26.11 and four CI platforms); the tag's CI green on every job; the
release published, immutable, not a draft, seven assets, attested and
verified; checksums match. Not checked: a floor job in CI (none exists),
running a release binary, the installers, and `update` end to end.
