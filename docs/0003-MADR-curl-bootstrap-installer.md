---
status: "accepted"
date: 2026-08-29
decision-makers: maccavelli
consulted: maccavelli
informed: maccavelli
---

# 0003-MADR: Ship a curl/PowerShell bootstrap installer that reuses magic-cli-remote's release-asset contract

## Context and Problem Statement

`mcp-server-recall` now publishes GitHub Release binaries on `vX.Y.Z` tags, with
unversioned aliases and `SHA256SUMS`, following magic-cli-remote's Go release
job. Installing still requires downloading an asset by hand and copying it, as
the README's Step 1 documents. There is no one-liner that turns a release into
a verified binary on `PATH`.

magic-cli-remote's advertised install is:

```sh
curl -fsSL https://github.com/maccavelli/magic-cli-remote/releases/latest/download/install.sh | sh
```

That script is ~1,080 lines. Most of it is service-manager logic
(systemd-user, launchd, runit, s6, OpenRC), WSL advisories, and
`setup-service` hand-off. Recall has no `setup-service`, no LaunchAgent, and
no user unit. Copying that file would install a daemon this binary does not
own.

The reusable part is the **bootstrap contract**, recorded in magic-cli-remote
MADRs 0097, 0104, and 0116:

* POSIX `sh` (Alpine ash) / PowerShell 5.1.
* No `sudo`. Install under `$HOME` / `%LOCALAPPDATA%`.
* No GitHub API call (60 req/hour anon limit).
* Download the **unversioned** alias; verify SHA-256 by **value** against
  `SHA256SUMS`, which lists **versioned** names.
* Wrap the body in `main` invoked on the last line so a truncated pipe cannot
  run a partial script.
* Piped invocation uses environment variables; flags exist for a saved copy.
* Same-filesystem staging (`mktemp` inside the install dir) so the final `mv`
  is atomic.
* A checksum mismatch installs nothing and leaves a pre-existing binary.

Recall's current release job already publishes the aliases and `SHA256SUMS`
that contract needs. It does **not** yet attach `install.sh` / `install.ps1`.

## Decision Drivers

* The README should install with one command, without a Go toolchain.
* Recall is one binary. A service-manager installer would be false advertising.
* The CI asset names already match magic-cli-remote convention C5:
  `mcp-server-recall-<goos>-<goarch>[-<VER>][.exe]`.
* Darwin/amd64 is not in this repo's matrix. The installer must refuse it, not
  guess Rosetta.
* Windows install belongs in PowerShell, not in the POSIX script.

## Considered Options

* **Copy magic-cli-remote `install.sh` and rewrite product names** — keep
  systemd/launchd/`setup-service`.
* **A slim bootstrap that keeps only the download/verify/install contract** —
  one product, no service setup, POSIX + PowerShell.
* **Document raw `curl -LO` of the alias** — no script, operator verifies
  checksums by hand.
* **Homebrew / scoop / winget first** — packaging instead of a release
  bootstrap.

## Decision Outcome

Chosen option: **"A slim bootstrap that keeps only the download/verify/install
contract"**, because that is the part of magic-cli-remote's one-liner that
applies to a single MCP server binary, and this repo's release assets already
satisfy it.

Unix one-liner:

```sh
curl -fsSL https://github.com/maccavelli/mcp-server-recall/releases/latest/download/install.sh | sh
```

Windows:

```powershell
irm https://github.com/maccavelli/mcp-server-recall/releases/latest/download/install.ps1 | iex
```

Mapping from magic-cli-remote, kept vs dropped:

| Kept | Dropped |
|---|---|
| Unversioned alias + SHA256SUMS by value | systemd / launchd / runit / s6 / OpenRC |
| No GitHub API, no sudo | `setup-service`, linger, FDA/TCC advisories |
| `main "$@"` last line; `MC_TEST_BASE_URL` | Two-product loop (`mcremote` + `mcrelay`) |
| `--version` / `MCP_RECALL_VERSION` | `--no-service`, `--with-relay-service` |
| `--dir` / `MCP_RECALL_INSTALL_DIR` | `mcremote update` hand-off |
| Darwin arm64 only; linux amd64+arm64; windows amd64 | darwin/amd64, windows/arm64 |
| Rename-aside (`.prev`) before replace | Stop-service-before-swap |

Defaults:

* Unix install dir: `$HOME/.local/bin` (same as `make install`).
* Windows install dir: `%LOCALAPPDATA%\Programs\mcp-server-recall`.
* Latest release via `.../releases/latest/download/...`; a pin uses
  `.../download/vX.Y.Z/...`.

The CI release job copies `scripts/install.sh` and `scripts/install.ps1` onto
the GitHub Release next to the binaries, exactly as magic-cli-remote does.

### Consequences

* Good, because a host with `curl` (or `wget`) and a SHA-256 tool can install
  without cloning or Go.
* Good, because verification cannot 404 on a versioned filename it does not
  yet know.
* Good, because a failed verify cannot clobber an existing install.
* Neutral, because upgrades are re-running the one-liner, not an in-binary
  updater. Recall has no `update` subcommand.
* Neutral, because the operator still runs `mcp-server-recall configure`
  afterwards; the installer does not write `recall.yaml`.
* Bad, because a running Darwin process holding the old Mach-O must be
  renamed aside (`.prev`) rather than overwritten. The installer does that;
  it does not stop MagicTools or launchd for the operator.
* Bad, because older GitHub Releases (before this change) have no `install.sh`
  asset. The one-liner only works starting with the first tag that ships it.

### Confirmation

1. `sh -n scripts/install.sh` and `shellcheck -s sh scripts/install.sh` are
   clean (or `shellcheck` is skipped only when the binary is absent, and the
   offline suite still runs).
2. `sh scripts/install_test.sh` passes offline, including Darwin-arm64
   accept, Darwin-amd64 reject, checksum mismatch leaving a pre-existing
   binary, and a successful linux/amd64 fake install.
3. The release job uploads `install.sh` and `install.ps1`.
4. README documents the Unix one-liner and the PowerShell one-liner as the
   primary install path.

## Pros and Cons of the Options

### Copy magic-cli-remote `install.sh` and rewrite product names

* Good, because the service paths are already battle-tested.
* Bad, because recall has no `setup-service`; the script would call a command
  that does not exist and then claim a service was configured.
* Bad, because ~700 lines of init detection would be dead weight and drift.

### A slim bootstrap that keeps only the download/verify/install contract

* Good, because it is the same user-visible one-liner with a script whose
  size matches the product.
* Good, because every invariant that makes the magic-cli-remote installer
  safe to pipe still holds.
* Neutral, because service installation stays a separate, later decision.
* Bad, because two codebases now share a contract by convention, not by a
  shared file.

### Document raw `curl -LO` of the alias

* Good, because there is no script to maintain.
* Bad, because operators will skip the checksum, and the README cannot name
  a versioned asset without 404s on `latest`.

### Homebrew / scoop / winget first

* Good, because upgrades and PATH are the package manager's job.
* Bad, because it does not ship on the next GitHub Release and needs taps
  or manifests this repository does not have.

## More Information

Implementation steps are in
[0003-PLAN-curl-bootstrap-installer.md](0003-PLAN-curl-bootstrap-installer.md).

magic-cli-remote sources: `scripts/install.sh`, `scripts/install.ps1`,
`scripts/install_test.sh`, `docs/0097-MADR-linux-curl-installer.md`,
`docs/0104-MADR-installer-linux-and-macos.md`, `docs/0116-MADR-windows-and-linux-arm64-build-targets.md`.
