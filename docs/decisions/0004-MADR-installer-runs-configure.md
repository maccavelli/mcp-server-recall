---
status: "accepted"
date: 2026-08-29
decision-makers: maccavelli
consulted: maccavelli
informed: maccavelli
---

# 0004-MADR: The installer runs configure --encrypt-db so a piped install finishes a usable store

## Context and Problem Statement

[0003](0003-MADR-curl-bootstrap-installer.md) ships a binary and tells the operator
to run `configure` next. A piped `curl | sh` cannot be interactive: stdin is the
script. If the installer invoked today's `configure` without a new flag, a TTY
check would see a pipe and either refuse (existing key) or write an unencrypted
store while printing success.

The operator asked for the installer to complete configuration, with
`--encrypt-db=true|false`. True must autogenerate a 64-character hex key.

## Considered Options

* **Leave configure interactive; installer only prints `next: configure`.**
* **Installer runs `configure --encrypt-db=true|false`, and `configure` gains
  that non-interactive flag.**
* **Installer writes `recall.yaml` itself.**

## Decision Outcome

Chosen option: **"Installer runs `configure --encrypt-db=true|false`"**, because
the wizard already owns YAML surgery, key validation, and Badger materialization.

* `--encrypt-db=true` generates a key when none exists; if a key is already in
  the file it is kept (no silent rotation).
* `--encrypt-db=false` writes a blank key. An existing key is refused unless
  `--allow-unencrypted` is also passed.
* Env key (`RECALL_ENCRYPTION_KEY` / `MCP_RECALL_ENCRYPTION_KEY`) still wins
  when set.
* Piped installer default is `--encrypt-db=true` so the one-liner produces an
  encrypted store. `--encrypt-db=false` and `MCP_RECALL_ENCRYPT_DB=false` opt
  out. `--no-configure` installs the binary only.

This amends 0003's statement that the installer does not write `recall.yaml`.
The installer still does not write YAML itself; it runs `configure`.

### Consequences

* Good, because `curl | sh` yields a verified binary, a config file, and a
  Badger `MANIFEST` without a second command.
* Good, because encryption is the default and key generation stays in Go
  (`crypto/rand`), not in the shell.
* Bad, because re-running the installer with `--encrypt-db=false` against a
  keyed config fails closed (same clobber guard as 0001).
* Neutral, because `--no-configure` restores the 0003 binary-only behaviour.

### Confirmation

1. `configure --encrypt-db=true` on a fresh sandbox writes a 64-hex key and a
   `MANIFEST`.
2. `configure --encrypt-db=false` writes `encryptionkey: ""`.
3. `--encrypt-db=true` does not rotate an existing key.
4. The installer default invokes `configure --encrypt-db=true`.
5. `--no-configure` does not invoke `configure`.
