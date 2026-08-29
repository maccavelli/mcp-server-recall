# Implement The installer runs configure --encrypt-db so a piped install finishes a usable store

Associated MADR: [0004-MADR-installer-runs-configure.md](0004-MADR-installer-runs-configure.md) (status: accepted)

## Goal

`curl | sh` installs the binary and completes `configure` non-interactively.
`--encrypt-db=true` (default) autogenerates a key; `--encrypt-db=false` writes
an unencrypted store.

## Scope

`cmd/mcp-server-recall/configure.go`, `configure_test.go`, `scripts/install.sh`,
`scripts/install.ps1`, `scripts/install_test.sh`, `README.md`.

## Implementation Steps

1. Add `configure --encrypt-db=true|false`. True generates a 64-hex key when
   none exists; false blanks only when no key is present (or `--allow-unencrypted`).
2. Installer default `MCP_RECALL_ENCRYPT_DB=true`; `--no-configure` skips.
3. Tests for generate / blank / keep-existing / clobber-refuse / installer args.

## Verification

```bash
go test ./cmd/mcp-server-recall/ -count=1 -run 'TestConfigure_EncryptDB'
sh -n scripts/install.sh && shellcheck -s sh scripts/install.sh
sh scripts/install_test.sh
```

## Rollout and Rollback

Ships with the next installer-bearing tag. `--no-configure` restores 0003
binary-only behaviour.
