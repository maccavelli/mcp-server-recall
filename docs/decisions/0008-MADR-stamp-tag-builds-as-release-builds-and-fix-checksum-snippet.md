---
status: "accepted"
date: 2026-11-17
decision-makers: maccavelli
consulted: "a scratch run of the released v2.1.1 darwin-arm64 binary (`--version`, `update --check` only); mcplib v1.4.1's selfupdate package and its 0005-MADR, read; this repository's workflow, Makefile and installers, read"
informed: maccavelli
---

# 0008-MADR: Stamp tag builds as release builds, hold it with a check, and correct the checksum snippet in the platform guide

## Context and Problem Statement

0007-PLAN's post-tag checks (its Amendment A2) found two older defects. The
owner ruled on both, as the coordinator relayed on 2026-11-17. The labels
are quoted exactly and only as labels; the text beside each says what was
asked.

* **D1, the build kind.** Chosen label: "Confirm with a scratch run, then
  fix all three and release recall v2.1.2 (Recommended)". Asked: three
  servers, this one among them, build release binaries with `make
  build-all VERSION="$TAG"` and no `BUILD_KIND=release`, so their release
  binaries are stamped as local builds; an agent confirms the refusal by
  running the downloaded v2.1.1 binary's read-only commands in a scratch
  directory; then each repository's tag build passes `BUILD_KIND=release`
  under a short record with a test or workflow check that holds it. This
  repository gets `v2.1.2` on the owner's word; the other two carry the fix
  into their next release. This record is this repository's part.
* **D2, the checksum snippet.** Chosen label: "Fix the guide to match the
  published names (Recommended)". Asked: one docs change, checked by running
  the corrected snippet against v2.1.1's real `SHA256SUMS` in a scratch
  directory; it may ride with the build-kind fix's record.

Everything in this record other than these quotations is evidence or the
author's text.

**Evidence index.** Read or measured on 2026-11-17 at HEAD `a9bfae3`.

* **E1, the cause [read].** `.github/workflows/ci.yml:124` runs `make
  build-all VERSION="$TAG"`. `Makefile:9` defaults `BUILD_KIND?=local`, and
  every build target stamps `-X main.RawBuildKind=$(BUILD_KIND)`
  (`Makefile:21-39`). The Makefile's comment (`Makefile:6-8`) says the
  release workflow passes it explicitly; it does not. `v2.1.1`'s own tag
  run logged `-X main.RawBuildKind=local` on all four targets.
  `cmd/mcp-server-recall/update.go:78-84` counts only the exact string
  "release" as a release build. `v2.1.0`'s workflow has the same line.
  `v2.0.0` has no `update` command (it arrived in `d80a55d`, between the
  two), so it is not affected. The Makefile comment's "(MADR 0005)" is
  mcplib's 0005-MADR, not this repository's 0005.
* **E2, the confirmation run [measured].** The `darwin-arm64` binary of
  release `v2.1.1`, downloaded earlier, first re-verified by `shasum -a 256
  -c SHA256SUMS` ("mcp-server-recall-darwin-arm64: OK"), was copied into a
  scratch directory used as `HOME`, and run through `env -i` with `HOME`,
  `TMPDIR` and all four `XDG_*` variables pointing into that directory, no
  `GH_TOKEN`, and a minimal `PATH`. Reading mcplib v1.4.1's `selfupdate`
  first showed what `update --check` does: it resolves the executable's
  own path (and requires it to sit under `HOME`, which is why the copy
  lives there), makes unauthenticated GitHub API requests for the release,
  and returns before any download, lock or write (`updater.go`: the
  `CheckOnly` branch precedes `apply`). Output:

  ```text
  $ mcp-server-recall --version            (exit 0)
  mcp-server-recall version 2.1.1
  $ mcp-server-recall update --check       (exit 10)
  selfupdate: resolving-target product=mcp-server-recall current=v2.1.1
  selfupdate: fetching-release product=mcp-server-recall current=v2.1.1
  selfupdate: selected product=mcp-server-recall current=v2.1.1 target=v2.1.1 asset=mcp-server-recall-darwin-arm64
  Error: selfupdate: update available
  ```

  A release build running `v2.1.1` against latest `v2.1.1` would say "up
  to date" and exit 0 (`version.go`: `cmp == 0` returns `OperationNone`);
  the local-build path returns `OperationReplaceLocal` whenever `--check`
  is used, whatever the versions, so exit 10 "update available" at the
  latest version is the local-build signature. The binary's only side
  effect was its startup `crash.log` inside the scratch directory. The real
  home was untouched: the mtimes of the product's real data, cache and
  install paths (read with `stat`, no contents) were identical before and
  after.
* **E3, what the build kind changes [read].** A search of non-test Go
  source finds `RawBuildKind` used only by `buildKind()` in `update.go`,
  which feeds `selfupdate.Request.CurrentBuild`. `--version`, the
  `User-Agent` (it carries `RawVersion` only) and logging do not use it.
  So the kind affects `update` and nothing else.
* **E4, why CI did not catch it [read].** The smoke steps assert the
  version string only: `ci.yml:242-244`, `got="$("./$f" --version 2>&1 |
  awk '{print $NF}')"` then `test "$got" = "$VER"`; the same in the `go` job
  (`:129-133`) and the `release` job (`:300-305`). The kind is not
  visible in `--version`.
* **E5, the user experience [read from code; only `--check` was run].**
  See the table in the PLAN's execution record (Amendment A1).
* **E6, the installers [read].** `scripts/install.sh:50-59` moves any
  existing binary aside to `.prev` and installs over it, and it reads no
  build kind (a search of the scripts finds none). `install.ps1` was read
  only for its version and checksum handling, not for its install step; the
  README's "installer run" advice rests on `install.sh`.
* **E7, the checksum snippet [measured].** `docs/guides/platform-installation.md`
  lines 154-155, 170-173, the macOS block near 211-214, and the Windows
  block 268-289 select a `SHA256SUMS` line with a versioned name
  (`mcp-server-recall-<os>-<arch>-<digit>…`). `SHA256SUMS` of `v2.1.1` and
  of `v2.1.0` list the exact basenames. The Linux snippet's `awk` on
  `v2.1.1`'s real file prints nothing, so its `test -n "$EXPECTED"` fails.
  `scripts/install.sh` accepts both shapes; the guide does not.

The question: how should tag builds carry the release stamp, what keeps it
from regressing, and what do users of `v2.1.0` and `v2.1.1` do?

## Decision Drivers

* The stamp must be explicit and made by the workflow (mcplib's 0005-MADR,
  "Version and Build Identity": "Release workflows stamp both facts
  explicitly through build variables").
* A local build that merely resembles a tag must stay "local"
  (`Makefile:6-8`), so the Makefile must not infer the kind from `VERSION`.
* The guard must fail before a tag, not at the tag: a tag, once pushed, makes
  an immutable release.
* No user-facing surface added without need; `--version`'s last word is
  parsed by the installers and CI (`awk '{print $NF}'`).
* The owner is the only user of the fleet: no compatibility engineering.

## Considered Options

* **A. The tag build passes `BUILD_KIND=release`** in `ci.yml`, as
  `mcp-server-duckduckgo`'s workflow does, plus a check on every push.
* **B. The Makefile infers "release" from a tag-shaped `VERSION`.**
* **C. Stamp in mcplib's reusable publish workflow.**
* **D. Change nothing; document `update --force`.**

## Decision Outcome

Chosen option: **A**, the owner's ruling D1.

1. **`ci.yml:124`** becomes `make build-all VERSION="$TAG" BUILD_KIND=release`.
2. **The check, on every push** (not tag-gated): a new `Release build
   flags` step in the `go` job runs a new script, `scripts/check_release_flags.sh`.
   It extracts the tag build's `make build-all` command from `ci.yml`,
   requires `BUILD_KIND=release` on it, runs `make -n` with that command's
   variables and requires all four targets' linker flags to carry
   `-X main.RawBuildKind=release`, and requires the same `make -n` without
   `BUILD_KIND` to carry `local` on all four (so the Makefile's default
   stays "local"). Where the host can run a Makefile-built binary (Linux
   amd64, as in CI, or macOS arm64) it also builds one target with
   `BUILD_KIND=release VERSION=v0.0.0-test` and runs `update --check` on
   it (see 3). It was seen to fail on planted breaks (PLAN, execution
   record).
3. **No user-facing surface is added.** A built binary does not expose its
   kind: `go version -m` does not record `-ldflags` here (measured on a
   scratch build with `BUILD_KIND=release` and without: identical
   `build` lines), and `--version` is parsed by its last word. A runtime
   probe exists without new surface, found in the scratch builds: a
   release-stamped binary validates its own version before any network
   call (`version.go`, `validateRequest`), so one built with the
   non-release version `v0.0.0-test` fails `update --check` at once with
   `"v0.0.0-test" is not a strict stable tag` (exit 1, no network), while
   a local-stamped one goes on to fetch the latest release and exits 10
   (both measured, scratch `HOME`). The script asserts the first. It
   cannot assert the second in CI without a network call, so a binary
   that lost the stamp makes the probe fail, possibly after a network
   request to the public release API.
4. **The guide** (`docs/guides/platform-installation.md`) selects the exact
   published names in all three snippets, and its descriptive text
   (lines 38-39, 154-155, 268-270) says so. The corrected Linux snippet's
   selection was run against `v2.1.1`'s real `SHA256SUMS`.
5. **Users of `v2.1.0` and `v2.1.1`.** Their binaries are local-stamped and
   stay so: `update` refuses until they pass `--force` once, or run the
   installer; the binary installed by either is a `v2.1.2` release build
   and updates normally afterwards. `README.md` ("Keeping it up to date")
   says this. The release's notes are generated by the reusable workflow
   and cannot carry it.
6. **A release follows**: `v2.1.2`, on the owner's word; the PLAN stops
   before the tag. The PLAN's post-publish check is the runtime proof: the
   published `v2.1.2` binary, run in a scratch directory exactly as in E2,
   reports "up to date" with exit 0 on `update --check`.

### Consequences

* Good, because every tag build from `v2.1.2` is a release build and
  `update` works as designed (`--check` exit 0 at the latest version).
* Good, because the check fails before a tag if the flag is lost or the
  Makefile's default drifts.
* Bad, because the probe builds one binary per push where the host can run
  it (the CI `go` job on Linux amd64), which costs one extra build, and
  because it exercises the Makefile's target with the flag given directly;
  the workflow's own command is checked by its expansion, and the tag
  build itself is proved only by the post-publish scratch run.
* Bad, because existing `v2.1.0` and `v2.1.1` installs need one `--force`
  or an installer run; no release can change them.
* Neutral, because `v2.1.2` changes no Go source: it carries the
  workflow, a script, and docs.
* Neutral, because the Makefile comment's "(MADR 0005)" stays ambiguous;
  it is not edited here.

### Confirmation

* `scripts/check_release_flags.sh` passes on the tree, and exits non-zero on
  scratch copies with the flag removed from `ci.yml`, with the Makefile
  default changed to `release`, with the windows target's flag hard-coded to
  `local`, and with the darwin target's (which also fails the runtime probe).
* `actionlint` on `ci.yml`; `make vet`, `make lint` with v2.15.0 under Go
  1.27.4 and 1.26.11, and the test suite pass.
* In a scratch copy, `make build-all VERSION=v0.0.0-test BUILD_KIND=release`
  builds, and the scratch darwin binary's `update --check` fails with
  `"v0.0.0-test" is not a strict stable tag` (exit 1, no network), while a
  build without the flag proceeds to the network and exits 10.
* The guide's corrected selection returns the real digest from `v2.1.1`'s
  `SHA256SUMS`.
* After the tag (owner's word): the tag's CI passes, and the post-publish
  scratch run reports exit 0 "up to date".

## Pros and Cons of the Options

### A. Pass the flag in the workflow (chosen)

* Good: the same shape as `mcp-server-duckduckgo`; explicit, as mcplib
  asks; one line.
* Bad: a person can drop the flag again; hence the check.

### B. The Makefile infers the kind from a tag-shaped `VERSION`

* Good: nothing for a workflow to forget.
* Bad: `Makefile:6-8` argues against it: a local build whose `git describe`
  resembles a tag (or `make VERSION=v1.2.3`) would claim to be a release and
  `update` would trust it. mcplib's 0005-MADR keeps the kind explicit.

### C. Stamp in the reusable workflow

* Good: fixes every caller at once.
* Bad: the reusable workflow does not build; it publishes artifacts this
  job already built. The stamp is in the binary before it gets there.

### D. Change nothing, document `--force`

* Good: no change, no release.
* Bad: every release binary stays a local build; `update --check` reports
  "update available" forever; the feature does not work as designed.

## More Information

### Open question for the owner

* **O1, how a binary shows its build kind.** A binary exposes no kind. The
  author implemented option (1) below, which adds no surface, and asks the
  owner to confirm or choose. Options: (1) the expansion check plus the
  version-validation probe and the post-publish scratch run (recommended;
  implemented; one extra build per push); (2) add a read-only way to see the
  kind (a `--version` second line breaks the `awk '{print $NF}'` parsers; a
  new `version` subcommand or `--build-kind` flag is new surface), which
  lets the tag build's smoke step assert the kind of the very binaries it
  ships; (3) the expansion check only, dropping the probe (cheapest, but
  nothing then runs a produced binary before the tag).

### Not verified

* That a `v2.1.2` binary built with the flag reports a release build at
  run time: shown for the command and expansion, and from source; the
  runtime proof is the post-publish scratch run.
* That `update --force` from a `v2.1.1` binary succeeds end to end: read
  from source (`version.go:91-110`), not run, and never run here.
* `install.ps1` behaviour beyond a read of its version handling.

### Links

* [0007-MADR](0007-MADR-move-toolchain-to-go-1-27-4-and-floor-to-1-26-11.md)
  and its PLAN Amendment A2, where the findings were first recorded.
* mcplib's 0005-MADR, "Version and Build Identity".
* [0008-PLAN](0008-PLAN-stamp-tag-builds-as-release-builds-and-fix-checksum-snippet.md).
