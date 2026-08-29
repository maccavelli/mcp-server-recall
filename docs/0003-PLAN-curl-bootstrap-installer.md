# Implement Ship a curl/PowerShell bootstrap installer that reuses magic-cli-remote's release-asset contract

Associated MADR: [0003-MADR-curl-bootstrap-installer.md](0003-MADR-curl-bootstrap-installer.md) (status: accepted)

## Goal

A host can install a checksum-verified `mcp-server-recall` from GitHub Releases
with one piped command, without Go, sudo, or the GitHub API.

## Scope

| Path | Change |
|---|---|
| `scripts/install.sh` | POSIX bootstrap |
| `scripts/install.ps1` | Windows bootstrap |
| `scripts/install_test.sh` | Offline suite |
| `.github/workflows/ci.yml` | Run the suite on push; attach scripts to the release |
| `README.md` | One-liners as the primary install |

Out of scope: systemd/launchd, an in-binary `update` command, Homebrew/winget,
darwin/amd64, windows/arm64.

## Implementation Steps

1. Write `scripts/install.sh` with `main "$@"` last, `MC_TEST_BASE_URL`, env
   equivalents of flags, value-based SHA-256 verify, `.prev` rename-aside,
   Darwin arm64 only.
2. Write `scripts/install.ps1` mirroring that contract for `windows/amd64` into
   `%LOCALAPPDATA%\Programs\mcp-server-recall`.
3. Write `scripts/install_test.sh` using a stub PATH and a fake `latest/download`
   tree.
4. CI: `sh scripts/install_test.sh` on the ubuntu `go` job. Release job copies
   both scripts onto the release.
5. README: Unix and Windows one-liners; keep manual copy as an alternative.

## Verification

```bash
sh -n scripts/install.sh
command -v shellcheck >/dev/null && shellcheck -s sh scripts/install.sh
sh scripts/install_test.sh
tail -1 scripts/install.sh   # exactly: main "$@"
```

Acceptance:

1. Offline suite green.
2. Checksum mismatch exits 2 and does not replace a pre-existing binary.
3. Release assets include `install.sh` and `install.ps1`.
4. README one-liner uses `releases/latest/download/install.sh`.

## Rollout and Rollback

Ships on the next `vX.Y.Z` tag. Older releases have no installer asset.
Rollback: revert the files; existing binaries are unaffected.
