# Getting started

This walkthrough takes `mcp-server-recall` from a new binary to an MCP client,
a working administrative CLI, and the dashboard.

## 1. Install the binary

Choose the path that matches what you need:

- For the current `main` branch, build from source with Go 1.26.5.
- For a prebuilt binary, manually install the latest release and verify its
  checksum.
- Do not use the release `install.sh` or `install.ps1` URLs yet: the latest
  release is v1.0.2 and does not contain those assets.

The complete OS-specific commands are in
[Platform installation](platform-installation.md).

Verify the installed program:

```bash
mcp-server-recall --version
mcp-server-recall --help
```

## 2. Create a secure configuration

The non-interactive command below creates the OS-native configuration and data
directories, generates a random 32-byte key, writes it to `recall.yaml`, and
materializes an empty Badger store:

```bash
mcp-server-recall configure --encrypt-db=true
```

Use `configure` without `--encrypt-db` for the terminal wizard:

```bash
mcp-server-recall configure
```

Use `--encrypt-db=false` only when plaintext Badger storage is intentional.
Disabling a key on an existing encrypted datastore does not decrypt it; the
store will no longer be readable with the changed configuration.

Configuration paths:

| OS | `recall.yaml` |
|---|---|
| Linux | `${XDG_CONFIG_HOME:-$HOME/.config}/mcp-server-recall/recall.yaml` |
| macOS | `$HOME/Library/Application Support/mcp-server-recall/recall.yaml` |
| Windows | `%APPDATA%\mcp-server-recall\recall.yaml` |

See [Configuration](configuration.md) before changing the generated file.

## 3. Choose a runtime mode

### MCP client-managed stdio

This is the normal MCP mode. Configure the client to execute the absolute
binary path with `serve` as its only argument:

```json
{
  "command": "/absolute/path/to/mcp-server-recall",
  "args": ["serve"]
}
```

The client owns the child process and communicates over stdin/stdout. Server
logs go to stderr so they do not corrupt JSON-RPC output. Use the full examples
in [Client integration](client-integration.md), because top-level keys vary by
client.

### Standalone local service

Run the same process yourself when using administrative CLI commands or the
dashboard:

```bash
mcp-server-recall serve
```

With no subcommand, the root command also runs `serve`, but using the explicit
subcommand is clearer in process managers and client configuration.

The service starts:

- the stdio MCP transport;
- `http://127.0.0.1:47669/mcp`;
- `http://127.0.0.1:47669/mcp/internal`;
- a UDP telemetry listener on the first available port from 49156–49160.

Set `MCP_ENDPOINT_API_PORT` before starting the server to change the HTTP port.
The `apiport` YAML key is currently ignored.

## 4. Verify the local service

Leave `serve` running and open a second terminal. The dashboard is the simplest
end-to-end check:

```bash
mcp-server-recall dash
```

The Summary page should change to `Server Connected` and show the selected UDP
port. Press `q` to exit.

An administrative CLI call also verifies the internal HTTP endpoint. Choose a
namespace that can safely return zero matches:

```bash
mcp-server-recall prune sessions 36500
```

The command should report that it connected to
`http://localhost:47669/mcp/internal`. It may prune zero records.

## 5. Try the core workflows

### Harvest a local Go project

```bash
mcp-server-recall harvest projects .
```

This requires the Go toolchain at runtime. If the server is launched by a GUI
with a restricted PATH, set `MCP_GO_BIN_PATH` in that client's server
environment.

### Harvest a standard-library package

```bash
mcp-server-recall harvest standards encoding/json
```

### Make a backup

Exports must be inside `exportdir` or the OS user-cache root. A portable pattern
is to configure a dedicated backup directory first; see
[Operations and security](operations-and-security.md#backup-and-restore).

### Inspect live operation

```bash
mcp-server-recall dash
```

Navigate with the up/down arrows or `k`/`j`. The Summary tab contains the event
tail; there is no separate Event Log tab in the current dashboard.

## Common first-run failures

| Symptom | Cause and resolution |
|---|---|
| `another instance is running` | A `serve` process already owns the singleton lock. Reuse or stop it. |
| CLI waits and then says the server is not running | Start `serve`, and make sure CLI and server use the same `MCP_ENDPOINT_API_PORT` or `MCP_REC_URL`. |
| `go toolchain not found` | Install Go or set `MCP_GO_BIN_PATH` to the absolute `go`/`go.exe` path. |
| `Path sandboxing violation` | Put the import/export file under configured `exportdir` or the OS user-cache root. |
| Store will not open after key change | Restore the exact prior encryption key or restore data from a plaintext JSONL export into a new store. |
| Dashboard shows persisted data but disconnected | The 10-second ring-file snapshot is readable, but live UDP telemetry is unavailable. |
