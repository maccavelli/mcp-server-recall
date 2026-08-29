# mcp-server-recall

`mcp-server-recall` is a local Model Context Protocol (MCP) server for durable
agent memory, structured records, Go source harvesting, full-text search, and
terminal observability. BadgerDB is the source of truth; Bleve provides BM25
content search, and `sahilm/fuzzy` adds character-subsequence matching for
record keys.

> Documentation status: audited against `main` on 2026-08-29 and updated for
> v1.1.0. The complete Go test suite passed during the audit.

## Install

The v1.1.0 bootstrap installers detect the supported OS and architecture,
verify the downloaded binary against `SHA256SUMS`, install without elevation,
and run `configure --encrypt-db=true` by default.

macOS or Linux:

```bash
curl -fsSL https://github.com/maccavelli/mcp-server-recall/releases/latest/download/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://github.com/maccavelli/mcp-server-recall/releases/latest/download/install.ps1 | iex
```

See [Platform installation](docs/guides/platform-installation.md) for supported
architectures, custom destinations, manual checksum verification, source
builds, and OS-specific paths.

## Table of contents

- [Install](#install)
- [What it does](#what-it-does)
- [I want to](#i-want-to)
- [Quick start](#quick-start)
- [Runtime model](#runtime-model)
- [Supported platforms](#supported-platforms)
- [Documentation library](#documentation-library)
- [Current limitations](#current-limitations)
- [Project verification](#project-verification)

## What it does

- Persists compressed records in eleven named BadgerDB domains.
- Exposes 17 MCP tools over stdio, with configurable subsets on two
  loopback-only Streamable HTTP endpoints.
- Searches record content with Bleve BM25 and record keys with fuzzy
  subsequence matching.
- Harvests exported symbols, signatures, documentation, examples, interface
  relationships, and dependencies from Go packages.
- Ingests Markdown, YAML, JSON, text, and XML files as searchable records.
- Exports and imports JSONL backups, prunes old records, rebuilds the search
  index, and displays live and persisted telemetry in a terminal dashboard.

This project does **not** currently create embeddings or run a vector database.
The words “semantic” and “vector” still appear in some CLI descriptions and
dashboard labels, but the implemented search path is lexical BM25 plus fuzzy
key matching. See the [repository assessment](docs/guides/repository-assessment.md).

## I want to

| Goal | Start here |
|---|---|
| Install on macOS, Linux, or Windows | [Platform installation](docs/guides/platform-installation.md) |
| Get a server running quickly | [Getting started](docs/guides/getting-started.md) |
| Connect an MCP client or VS Code | [Client integration](docs/guides/client-integration.md) |
| Configure paths, encryption, search, or tool exposure | [Configuration](docs/guides/configuration.md) |
| Use `configure`, `serve`, `dash`, `harvest`, backups, or cleanup commands | [CLI reference](docs/guides/cli-reference.md) |
| Call the MCP tools or understand namespaces | [MCP tools and namespaces](docs/guides/mcp-tools.md) |
| Read the dashboard and telemetry | [Dashboard and observability](docs/guides/dashboard-and-observability.md) |
| Back up data or understand security boundaries | [Operations and security](docs/guides/operations-and-security.md) |
| Understand what is complete, partial, or misleading | [Repository assessment](docs/guides/repository-assessment.md) |

## Quick start

The most reliable way to run the current `main` branch is to build it with the
Go version declared by [`go.mod`](go.mod):

```bash
git clone https://github.com/maccavelli/mcp-server-recall.git
cd mcp-server-recall
go build -o mcp-server-recall ./cmd/mcp-server-recall
./mcp-server-recall configure --encrypt-db=true
./mcp-server-recall serve
```

On Windows PowerShell, build an `.exe` and use the call operator:

```powershell
git clone https://github.com/maccavelli/mcp-server-recall.git
Set-Location mcp-server-recall
go build -o mcp-server-recall.exe ./cmd/mcp-server-recall
& .\mcp-server-recall.exe configure --encrypt-db=true
& .\mcp-server-recall.exe serve
```

`serve` uses the terminal for MCP stdio and remains in the foreground. Normally
an MCP client starts this process for you. Run `dash`, `harvest`, `export`,
`import`, or `prune` from a second terminal while `serve` is running.

The bootstrap installer is the preferred release installation path. Manual
verified downloads and current-source builds remain available in the platform
guide.

## Runtime model

| Component | Implemented behavior |
|---|---|
| Primary transport | MCP over stdio; the client launches `mcp-server-recall serve` |
| Local service | Streamable HTTP on `127.0.0.1:47669` by default |
| HTTP endpoints | `/mcp` for the configured safe subset; `/mcp/internal` for local administrative clients |
| Source of truth | BadgerDB records, Zstd-compressed before storage |
| Search | Persistent Bleve index for BM25 plus fuzzy key matching |
| Telemetry | UDP loopback updates every 500 ms and `telemetry.ring` snapshots every 10 seconds |
| Process model | One `serve` process per host, enforced with a temporary-directory lock |

The binary runs without Go. The Go toolchain is required only to build from
source and to use either `harvest` subcommand. Current source development
requires Go 1.26.5.

## Supported platforms

| Platform | Current `main` build/CI | v1.1.0 binary |
|---|---:|---:|
| Linux amd64 | Yes | Yes |
| Linux arm64 | Yes | Yes |
| macOS arm64 (Apple silicon) | Yes | Yes |
| macOS amd64 (Intel) | Not published by the project | No |
| Windows amd64 | Yes | Yes |
| Windows arm64 | Not published by the project | No |

All release builds are configured with `CGO_ENABLED=0`. Platform-specific
paths, checksums, PATH setup, and Go discovery are covered in the
[platform guide](docs/guides/platform-installation.md).

## Documentation library

1. [Getting started](docs/guides/getting-started.md) — first configuration,
   runtime modes, and a working smoke test.
2. [Platform installation](docs/guides/platform-installation.md) — macOS,
   Linux, and Windows walkthroughs.
3. [Client integration](docs/guides/client-integration.md) — verified stdio
   configuration, including current VS Code syntax.
4. [Configuration](docs/guides/configuration.md) — active YAML fields,
   environment variables, real examples, and path defaults.
5. [CLI reference](docs/guides/cli-reference.md) — every command, dependency,
   safety behavior, and example.
6. [MCP tools and namespaces](docs/guides/mcp-tools.md) — all tools, endpoint
   exposure, namespace support, and request examples.
7. [Dashboard and observability](docs/guides/dashboard-and-observability.md) —
   tabs, metrics, controls, transports, and known display caveats.
8. [Operations and security](docs/guides/operations-and-security.md) — backups,
   pruning, encryption scope, local HTTP boundaries, and recovery.
9. [Repository assessment](docs/guides/repository-assessment.md) — evidence,
   feature maturity, release state, gaps, and prior README corrections.

## Current limitations

- Search is lexical and fuzzy, not embedding-based semantic/vector search.
- Go harvesting requires a resolvable Go executable and may download remote
  modules into the Go module cache.
- The generated YAML includes `apiport`, `badgerdb`, `bleveindex`, and several
  harvest/ingest settings that the current runtime does not consume.
- Badger data may be encrypted, but `recall.yaml` stores the key as plaintext,
  Bleve has no configured encryption layer, and JSONL exports are plaintext.
- The loopback HTTP service has no authentication. Do not proxy, port-forward,
  or rebind it to a non-loopback interface.
- Several dashboard labels are static or heuristic rather than measured. The
  security tab is not an encryption attestation.
- Prebuilt Intel macOS and Windows arm64 binaries are not published.

## Project verification

The repository audit used:

```bash
go build ./cmd/mcp-server-recall
go test ./...
sh scripts/install_test.sh
```

The codebase also defines CI checks for formatting, `go mod tidy`, `go vet`,
race-enabled tests, cgo-free tests, linting, installer tests, cross-builds, and
native Linux arm64/Windows amd64 execution. See
[`.github/workflows/ci.yml`](.github/workflows/ci.yml) for the executable source
of truth.

Authoritative external references used by this documentation include the
[Go downloads page](https://go.dev/dl/),
[Go OS directory semantics](https://pkg.go.dev/os#UserConfigDir),
[the MCP transport specification](https://modelcontextprotocol.io/specification/draft/basic/transports),
[VS Code's MCP configuration reference](https://code.visualstudio.com/docs/agents/reference/mcp-configuration),
and the [project's latest GitHub release](https://github.com/maccavelli/mcp-server-recall/releases/latest).
