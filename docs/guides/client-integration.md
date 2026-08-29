# Client integration

`mcp-server-recall` supports the two standard MCP transports: stdio and
Streamable HTTP. Use stdio for normal desktop/IDE integration. Use the HTTP
endpoint only for a separately running local server.

## Required command shape

Every stdio client must start the binary with the `serve` argument:

```text
/absolute/path/to/mcp-server-recall serve
```

Use an absolute binary path. GUI applications often do not inherit the same
PATH, HOME, or Go environment as an interactive shell.

## Visual Studio Code

Current VS Code MCP configuration uses a top-level `servers` object, not the
`mcpServers` object shown in the previous README. VS Code can open the correct
user-level file with **MCP: Open User Configuration**, or a workspace file with
**MCP: Open Workspace Folder MCP Configuration**. The workspace form is
`.vscode/mcp.json`.

The format below follows the
[official VS Code MCP configuration reference](https://code.visualstudio.com/docs/agents/reference/mcp-configuration).

### Linux

```json
{
  "servers": {
    "recall": {
      "type": "stdio",
      "command": "/home/alex/.local/bin/mcp-server-recall",
      "args": ["serve"],
      "env": {
        "MCP_GO_BIN_PATH": "/usr/local/go/bin/go"
      }
    }
  }
}
```

### macOS

```json
{
  "servers": {
    "recall": {
      "type": "stdio",
      "command": "/Users/alex/.local/bin/mcp-server-recall",
      "args": ["serve"],
      "env": {
        "MCP_GO_BIN_PATH": "/usr/local/go/bin/go"
      }
    }
  }
}
```

### Windows

JSON requires doubled backslashes:

```json
{
  "servers": {
    "recall": {
      "type": "stdio",
      "command": "C:\\Users\\alex\\AppData\\Local\\Programs\\mcp-server-recall\\mcp-server-recall.exe",
      "args": ["serve"],
      "env": {
        "MCP_GO_BIN_PATH": "C:\\Program Files\\Go\\bin\\go.exe"
      }
    }
  }
}
```

`MCP_GO_BIN_PATH` is optional if harvesting is not used or the client can
already resolve `go`. After saving, run **MCP: List Servers** and use its output
view to inspect startup failures.

## Clients that use `mcpServers`

Some MCP clients use a Claude-style top-level `mcpServers` object. For those
clients, keep the server entry itself the same:

```json
{
  "mcpServers": {
    "recall": {
      "command": "/absolute/path/to/mcp-server-recall",
      "args": ["serve"],
      "env": {
        "MCP_GO_BIN_PATH": "/absolute/path/to/go"
      }
    }
  }
}
```

Do not copy this wrapper into VS Code; its current schema uses `servers`.
Client file locations and packaging change independently of this repository, so
use the client's current official instructions to locate its configuration.

Claude Desktop now emphasizes desktop extensions (`.mcpb`) for local MCP
servers. This repository does not currently ship an `.mcpb` package. If the
installed Claude Desktop version still exposes local developer configuration,
use the `mcpServers` entry above; otherwise a desktop-extension package must be
created before one-click installation is possible. Anthropic's current workflow
is documented in [Getting Started with Local MCP Servers on Claude Desktop](https://support.claude.com/en/articles/10949351-getting-started-with-local-mcp-servers-on-claude-desktop).

## Environment variables in client configuration

Useful per-server values include:

| Variable | When to set it |
|---|---|
| `MCP_GO_BIN_PATH` | The client cannot find `go` and harvesting is needed. |
| `MCP_ENDPOINT_API_PORT` | Port 47669 conflicts with another local service. |
| `MCP_RECALL_DBPATH` | This client should use a non-default datastore. |
| `MCP_RECALL_ENCRYPTION_KEY` | You intentionally keep the key out of YAML and accept client-managed secret injection. |
| `GOMEMLIMIT` | Override the binary's default `1024MiB`. |
| `GOMAXPROCS` | Override the binary's default `2`. |

When overriding the HTTP port, the MCP-client-managed `serve` process and any
separate CLI process must receive matching settings. For the CLI, either export
the same `MCP_ENDPOINT_API_PORT` or set a complete `MCP_REC_URL`.

## Streamable HTTP

When `serve` is already running, these endpoints are available on loopback:

```text
http://127.0.0.1:47669/mcp
http://127.0.0.1:47669/mcp/internal
```

- `/mcp` exposes `safetools` plus `get_internal_logs`.
- `/mcp/internal` exposes `safetools_internal` plus `get_internal_logs`.
- Both are bound to `127.0.0.1` and additionally reject non-loopback peers.
- Neither endpoint implements authentication.

The `/mcp` endpoint is called “readonly” in one internal variable, but the
default `safetools` includes `save_to_recall`; treat it as a restricted endpoint,
not a read-only endpoint.

Do not expose either endpoint through a reverse proxy, SSH remote forward,
container port publish, or non-loopback bind. The MCP transport specification
recommends Origin validation and authentication for HTTP servers; this
implementation relies on loopback isolation and has neither control.

## Standalone CLI connectivity

`harvest`, `export`, `import`, and `prune` connect to:

```text
${MCP_REC_URL:-http://localhost:${MCP_ENDPOINT_API_PORT:-47669}/mcp}/internal
```

Examples:

```bash
MCP_ENDPOINT_API_PORT=48669 mcp-server-recall serve
```

In another shell:

```bash
MCP_ENDPOINT_API_PORT=48669 mcp-server-recall prune sessions 30
```

Or override the whole base URL:

```bash
MCP_REC_URL=http://localhost:48669/mcp mcp-server-recall export /allowed/path/backup.jsonl
```

`MCP_REC_URL` must name the base `/mcp` URL because the CLI appends `/internal`.

## Troubleshooting

- Confirm the configured binary path exists from the same user account as the
  client.
- Confirm `args` contains `serve`.
- Look at the client's MCP output log; server logs on stderr are normal.
- Inspect the application crash log listed in
  [Platform installation](platform-installation.md).
- A second `serve` process will fail because the server enforces a singleton
  lock in the OS temporary directory.
- If tools changed, restart the MCP server. VS Code also provides
  **MCP: Reset Cached Tools**.
