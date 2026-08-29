# CLI reference

The command tree is implemented with Cobra. Help and version information are
written to stderr to protect stdout for the stdio JSON-RPC transport.

## Command overview

| Command | Requires running `serve` | Requires Go | Destructive |
|---|---:|---:|---:|
| `configure` / `init` | No | No | Can overwrite config or make encrypted data unreadable |
| `serve` | No | No | No |
| `dash` | No, but live data does | No | No |
| `harvest standards` | Yes | Yes | Writes/updates `standards` |
| `harvest projects` | Yes | Yes | Writes/updates `projects` |
| `export` | Yes | No | No; refuses to overwrite a file |
| `import` | Yes | No | Upserts records with matching keys |
| `prune` | Yes | No | Yes |
| `purge` | No; stop it first | No | Yes, removes the whole datastore directory |
| `completion` | No | No | No |

Running `mcp-server-recall` with no subcommand invokes `serve`.

## `configure` (`init` alias)

```text
mcp-server-recall configure [flags]
```

| Flag | Effect |
|---|---|
| `--encrypt-db=true` | Non-interactively preserve an existing key or generate a new 32-byte key. |
| `--encrypt-db=false` | Non-interactively configure an empty key. |
| `--allow-unencrypted` | Permit non-interactive removal of an existing key. |
| `--force`, `-f` | Rewrite `recall.yaml` from the full default template. |

Without `--encrypt-db`, an attached terminal starts the interactive encryption
wizard. A non-interactive run with no key proceeds unencrypted. If a key already
exists, the non-interactive guard refuses to erase it unless
`--allow-unencrypted` is explicit.

Key environment precedence for the wizard is:

1. `RECALL_ENCRYPTION_KEY`
2. `MCP_RECALL_ENCRYPTION_KEY`
3. `MCP_RECALL_ENCRYPTIONKEY`

The key must be exactly 64 hexadecimal characters. Configuration is written
atomically and then reloaded before the datastore is materialized.

Examples:

```bash
mcp-server-recall configure --encrypt-db=true
mcp-server-recall configure
mcp-server-recall configure --force --encrypt-db=true
mcp-server-recall init --encrypt-db=false
```

## `serve`

```text
mcp-server-recall serve
```

Starts all runtime components:

- MCP stdio transport;
- loopback Streamable HTTP at `/mcp` and `/mcp/internal`;
- BadgerDB and optional Bleve index;
- 10-second persisted telemetry snapshots;
- 500 ms UDP dashboard telemetry;
- background value-log GC and search-index audit work.

Only one instance can hold the process lock. `SIGINT` and `SIGTERM` trigger
graceful shutdown. HTTP shutdown gets 5 seconds; store workers get 15 seconds by
default, configurable with `MCP_RECALL_SHUTDOWN_TIMEOUT_SECS`.

The active HTTP port is controlled by `MCP_ENDPOINT_API_PORT`, not the YAML
`apiport` field.

## `dash`

```text
mcp-server-recall dash
```

Launches the alternate-screen terminal UI. It can show the last persisted
`telemetry.ring` snapshot without a server, but live values require `serve`.

Controls:

- Up/down arrows or `k`/`j`: navigate.
- Enter on Quit: exit.
- `q` or Ctrl+C: exit immediately.

See [Dashboard and observability](dashboard-and-observability.md).

## `harvest standards`

```text
mcp-server-recall harvest standards <package-path>
```

Examples:

```bash
mcp-server-recall harvest standards encoding/json
mcp-server-recall harvest standards github.com/modelcontextprotocol/go-sdk/mcp
```

The command connects to `/mcp/internal` and calls the `harvest` tool. The server
uses `go/packages`, `go doc -all`, AST inspection, and Go types. Remote package
resolution may run `go mod init`, `go get ...@latest`, and `go list` in a
temporary workspace and populate the user's Go module cache.

## `harvest projects`

```text
mcp-server-recall harvest projects <local-directory>
```

Example:

```bash
mcp-server-recall harvest projects .
```

Relative paths beginning with `.`, `..`, or `/` are converted to absolute paths
by the CLI. On Windows, pass an absolute path when in doubt:

```powershell
mcp-server-recall.exe harvest projects (Get-Location).Path
```

Despite the general “codebase” wording, this command performs Go AST/type
harvesting. It is not a language-independent source indexer.

## Go executable resolution

The server resolves Go once per process in this order:

1. `MCP_GO_BIN_PATH` exactly as provided.
2. `$GOROOT/bin/go` or `go.exe` if it is executable.
3. `~/sdk/go*/bin/go`, known SDK directories, `~/.local/go/bin/go`,
   `~/go/bin/go`, and on Unix `/usr/local/go/bin/go` or `/usr/lib/go/bin/go`.
4. PATH lookup.

Some error strings still mention `RECALL_GO_BIN`, but the resolver does not read
that variable. Use `MCP_GO_BIN_PATH`.

## `export`

```text
mcp-server-recall export <filepath>
```

The CLI converts the path to absolute, connects to the running internal
endpoint, and requests a JSONL export of all non-index records. Server-side
sandboxing permits only paths under configured `exportdir` or the OS
user-cache root.

The destination must not exist; the writer uses exclusive creation.

Linux example:

```bash
mkdir -p "$HOME/.cache/mcp-server-recall/backups"
mcp-server-recall export "$HOME/.cache/mcp-server-recall/backups/recall-2026-08-29.jsonl"
```

macOS example:

```bash
mkdir -p "$HOME/Library/Caches/mcp-server-recall/backups"
mcp-server-recall export "$HOME/Library/Caches/mcp-server-recall/backups/recall-2026-08-29.jsonl"
```

Windows example:

```powershell
$backupDir = Join-Path $env:LOCALAPPDATA 'mcp-server-recall\backups'
New-Item -ItemType Directory -Force $backupDir | Out-Null
mcp-server-recall.exe export (Join-Path $backupDir 'recall-2026-08-29.jsonl')
```

Exports are plaintext even when Badger encryption is enabled.

## `import`

```text
mcp-server-recall import <filepath>
```

Imports a JSONL export in batches of 100 and preserves timestamps for keys that
do not already exist. It does **not** wipe the database. Existing keys are
updated; their existing creation time is retained by the batch-write path.
Partial line errors are reported while valid records can still be stored.

The file must satisfy the same server-side sandbox as exports. Stop and back up
the target datastore before a large or untrusted import.

## `prune`

```text
mcp-server-recall prune <namespace> [days]
```

Examples:

```bash
mcp-server-recall prune sessions
mcp-server-recall prune server_status 14
mcp-server-recall prune dialectic_history 90
```

The CLI accepts one namespace and an optional non-negative number of days. If
omitted, the CLI sends 30; it does not read `defaultpurgedays` for this default.
The server then runs `prune_records` through `/mcp/internal`.

The age applies to session-like namespaces. `memories`, `standards`, and
`projects` use their own semantic or structural vacuum routines, so the days
argument is accepted but does not control those three routines.

Use the MCP tool directly for report-only and consolidation options. The CLI
does not expose those arguments.

## `purge`

```text
mcp-server-recall purge [--force]
```

Removes the entire configured datastore directory, including Badger data,
Bleve search data, and `telemetry.ring`. The command refuses empty/unsafe paths,
the working directory or its parents, and directories without a Badger
`MANIFEST`.

Without `--force`, an attached terminal must confirm. Non-interactive use
requires `--force`.

Stop `serve`, create a backup, and resolve the exact `dbpath` before running:

```bash
mcp-server-recall purge
mcp-server-recall configure --encrypt-db=true
```

`purge` does not remove `recall.yaml`; rerunning `configure` is useful when a
fresh materialized store is desired.

## `completion`

Cobra provides completion generation automatically:

```bash
mcp-server-recall completion bash
mcp-server-recall completion zsh
mcp-server-recall completion fish
mcp-server-recall completion powershell
```

Run `mcp-server-recall completion <shell> --help` for shell-specific loading
instructions.
