---
status: in-progress
date: 2026-11-17
associated-madr: "0008-MADR-stamp-tag-builds-as-release-builds-and-fix-checksum-snippet.md"
---

# 0008-PLAN: Pass `BUILD_KIND=release` in the tag build, hold it with a check, correct the checksum snippet, and prepare `v2.1.2`

Associated MADR:
[0008-MADR-stamp-tag-builds-as-release-builds-and-fix-checksum-snippet.md](0008-MADR-stamp-tag-builds-as-release-builds-and-fix-checksum-snippet.md)
(option A, as the owner ruled).

## Goal

* Tag builds stamp `RawBuildKind=release`.
* A check on every push fails if the command or the Makefile default drifts.
* The platform guide's checksum snippets select the published names.
* `README.md` tells users of `v2.1.0` and `v2.1.1` what to do once.
* `v2.1.2` is ready to tag. The plan stops before the tag; the tag needs the
  owner's word.

## Scope

| File | Change |
| :--- | :--- |
| `.github/workflows/ci.yml` | line 124 gains `BUILD_KIND=release`; a new always-on step `Release build flags` in the `go` job runs the script |
| `scripts/check_release_flags.sh` | new: the check |
| `docs/guides/platform-installation.md` | the three checksum snippets and the text at lines 38-39, 154-155, 268-270 |
| `README.md` | one paragraph under "Keeping it up to date" for local-stamped installs |
| `docs/0008-*` | this pair |

Out of scope: the other two servers; the Makefile comment's "(MADR 0005)";
the tag and release (owner's word); any change to the Go source, `--version`
or any user-facing command.

## Rules

* Edits by a script or the edit tool, each old string counted; results read
  back. No heredocs. Heavy runs through `run-heavy`.
* The released binary was run only as `--version` and `update --check`, in a
  scratch `HOME`; never `update` without `--check`, never `serve`, never
  `--force`.
* A finding stops the phase with evidence.

## Implementation Steps

1. **Confirm** (done; MADR E2).
2. **Check script and workflow edit**; run the script on the tree and on
   three scratch copies with planted breaks (flag removed from `ci.yml`;
   the Makefile default set to `release`; one target's flag dropped).
3. **Guide**: correct the three snippets; run the Linux selection against
   `v2.1.1`'s real `SHA256SUMS` in a scratch directory, and the same
   selection logic for the other two names.
4. **README paragraph.**
5. **Gates:** `actionlint`, `sh scripts/install_test.sh`, `make vet`, lint
   with the scratch v2.15.0 under Go 1.27.4 and `GOTOOLCHAIN=go1.26.11`,
   `go test ./...` through `run-heavy`, markdownlint on the changed docs, a
   scratch `make build-all VERSION=v0.0.0-test BUILD_KIND=release` and one
   without the flag, `--version` of each scratch-built binary.
6. **Stage** the pair by path; the change files stay unstaged.
7. **Release `v2.1.2`** (after the coordinator commits and pushes, CI is
   green and the owner gives the word): tag `mcp-server-recall v2.1.2`
   (annotated, unsigned, as `v2.1.0` and `v2.1.1`). After publish,
   read-only: the tag's CI green; the release published, immutable, seven
   assets; `gh release verify` exit 0; checksums match; then the runtime
   proof: the published `darwin-arm64` binary, verified against
   `SHA256SUMS`, in a scratch `HOME` as in MADR E2: `--version` prints
   `2.1.2` and `update --check` exits 0 "up to date" (a local-stamped one
   exits 10).

## Verification

* V1 the script passes on the tree and fails on three planted copies.
* V2 `actionlint` and every gate pass.
* V3 the scratch builds: with the flag the binary carries `release`, without
  it `local`.
* V4 the guide's selection returns the real digest.
* V5 after the tag: the runtime proof (step 7).

## Rollout and Rollback

Before the tag, revert the change commit; the records stay. After the tag, a
release is immutable: a fault is fixed by `v2.1.3`. Users on `v2.1.0` or
`v2.1.1` change only when they run `update --force` or an installer.

## Execution Record

All on 2026-11-17 at HEAD `a9bfae3` plus this change, `GOPROXY=off`. Exit
statuses were taken before any filter.

### Step 1: the confirmation run (MADR E2)

Real-home mtimes of the product's data, cache and install paths were read
with `stat` before and after (no contents): identical. The scratch run's
output is in the MADR. `--version`: exit 0, "mcp-server-recall version
2.1.1". `update --check`: exit 10, ends "Error: selfupdate: update
available" with `current=v2.1.1 target=v2.1.1`: a local-stamped binary at
the latest version.

### The user's experience today, by command (release binaries v2.1.0 and v2.1.1)

| Command | What happens | Basis |
| :--- | :--- | :--- |
| `update --check` | exit 10 "update available" at every version, including the latest | run |
| `update` | after resolving the release, fails: "selfupdate: replacing a local build requires --force to install vX.Y.Z", exit 1 | code (`version.go:91-97`) |
| `update --yes` | the same refusal (`--yes` skips the prompt, not the force requirement) | code |
| `update --version vX.Y.Z` | the same refusal, upgrades and rollbacks alike | code |
| `update --force` | asks, then replaces the binary with the latest release (or the pinned one); the replacement is again local-stamped until a release built with the fix | code (`version.go:98-110`) |
| `scripts/install.sh` | installs the latest or pinned release over the old one, moving it to `.prev`; no build kind involved | code |

The kind affects only `update` (MADR E3). `v2.0.0` has no `update`.

### Steps 2 to 4: the change

* `ci.yml`: line 124 now `make build-all VERSION="$TAG" BUILD_KIND=release`;
  a new always-on step "Release build flags" in the `go` job runs
  `shellcheck` and `scripts/check_release_flags.sh`.
* `scripts/check_release_flags.sh`: new (the MADR's check).
* `docs/guides/platform-installation.md`: Linux, macOS and Windows snippets
  select the exact name; the text at the installer list and the two
  "Manual verified download" introductions agrees.
* `README.md`: a paragraph for `v2.1.0` and `v2.1.1` installs.
* Red first: before the workflow edit the script failed on the real defect
  ("ci.yml's tag build does not pass BUILD_KIND=release", "expands to 4
  builds, 0 release, 4 local"), exit 1.

### Planted failures of the check (scratch copies of the changed tree)

| Plant | Result |
| :--- | :--- |
| flag removed from `ci.yml` | exit 1: "does not pass BUILD_KIND=release"; "4 builds, 0 release, 4 local" |
| Makefile default `BUILD_KIND?=release` | exit 1: "default build expands to 4 release, 0 local (want 0, 4)" |
| windows target's stamp hard-coded to `local` | exit 1: "4 builds, 3 release, 1 local" |
| darwin target's stamp hard-coded to `local` | exit 1: the same expansion failure, and "built mcp-server-recall-darwin-arm64 is not a release build: update --check did not fail version validation (exit 1)" |

On the tree: exit 0, four `ok` lines (the last is the runtime probe).
It also passes under `dash`.

### The guide's snippets against v2.1.1's real `SHA256SUMS`

The lines taken from the guide, run on the downloaded real file: Linux
amd64 expected `ecf43a3b…4716095` and arm64 `d7ce0664…6d24e`, each equal to
the digest of the real downloaded binary; macOS arm64 `985a53d3…3647`,
equal; Windows (PowerShell, run with `pwsh`) `b636f762…99c0e4a`, equal. The
old Linux snippet's `awk` on the same file printed nothing. A changed
asset name in the PowerShell lines throws.

### Scratch builds (`make build-all VERSION=v0.0.0-test`, scratch copy, scratch `HOME`)

| Build | `--version` | `update --check` |
| :--- | :--- | :--- |
| `BUILD_KIND=release` | "mcp-server-recall version 0.0.0-test" | exit 1: `selfupdate: "v0.0.0-test" is not a strict stable tag` (no network) |
| no flag | the same | exit 10: resolves and fetches the release, "update available" |

### Gates

| Gate | Start | End | Exit |
| :--- | :--- | :--- | :--- |
| `actionlint ci.yml` | 15:21:12 | 15:21:12 | 0 |
| `shellcheck -s sh scripts/check_release_flags.sh` | 15:21:12 | 15:21:12 | 0 |
| `sh scripts/check_release_flags.sh` | 15:21:12 | 15:21:12 | 0 (re-run on the final script: 0) |
| `sh scripts/install_test.sh` | 15:21:12 | 15:21:20 | 0 |
| `gofmt -l cmd internal` empty | 15:21:20 | 15:21:20 | 0 |
| `go vet ./...` | 15:21:20 | 15:21:21 | 0 |
| `make lint`, v2.15.0, Go 1.27.4 | 15:21:21 | 15:21:22 | 0 |
| `make lint`, v2.15.0, `GOTOOLCHAIN=go1.26.11` | 15:21:22 | 15:21:24 | 0 |
| `go mod tidy -diff` | 15:21:24 | 15:21:24 | 0 |
| markdownlint, README and the guide | 15:21:24 | 15:21:24 | 0 |
| `git diff --check` | 15:21:24 | 15:21:24 | 0 |
| `go test ./...` via `run-heavy`, 1.27.4 | 15:21:24 | 15:22:17 | 0 |
| `go test ./...` via `run-heavy`, `go1.26.11` | 15:22:17 | 15:23:10 | 0 |

No Go file changed, so the vet, lint and test results equal those at `a9bfae3`.

## Deviations

None.
