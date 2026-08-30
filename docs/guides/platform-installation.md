# Platform installation

This guide covers the supported v1.1.0 release installers, manual verified
downloads, source builds, filesystem locations, upgrades, and troubleshooting
for Linux, macOS, and Windows.

## Installation at a glance

| Platform | Release target | One-line installer | Default binary directory |
|---|---|---|---|
| Linux x86-64 | `linux-amd64` | Yes | `$HOME/.local/bin` |
| Linux arm64 | `linux-arm64` | Yes | `$HOME/.local/bin` |
| macOS Apple silicon | `darwin-arm64` | Yes | `$HOME/.local/bin` |
| macOS Intel | Not published | No | Source build only |
| Windows x86-64 | `windows-amd64` | Yes | `%LOCALAPPDATA%\Programs\mcp-server-recall` |
| Windows arm64 | Not published | Installer refuses emulation | Source build or intentional manual amd64 use |

Release binaries are built with `CGO_ENABLED=0`; running a prebuilt binary does
not require Go. Go is required only to build from source.

## Recommended one-line installation

### Linux or macOS

```bash
curl -fsSL https://github.com/maccavelli/mcp-server-recall/releases/latest/download/install.sh | sh
```

### Windows PowerShell

```powershell
irm https://github.com/maccavelli/mcp-server-recall/releases/latest/download/install.ps1 | iex
```

The installers:

1. Detect the operating system and architecture.
2. Download the unversioned alias for the selected release binary.
3. Find its versioned entry in `SHA256SUMS` and verify the hash value.
4. Move an existing binary to `.prev` before installing the replacement.
5. Install per-user without `sudo` or administrator elevation.
6. Run `configure --encrypt-db=true` unless configuration is disabled.

Neither installer silently edits PATH. It prints the required PATH change when
the destination is not already available to the current process or user.

## Installer options

### Linux and macOS options

A command piped into `sh` cannot receive positional flags after the pipe. Use
the installer environment variables instead.

Pin v1.1.0:

```bash
curl -fsSL https://github.com/maccavelli/mcp-server-recall/releases/latest/download/install.sh \
  | MCP_RECALL_VERSION=1.1.0 sh
```

Choose a different directory:

```bash
curl -fsSL https://github.com/maccavelli/mcp-server-recall/releases/latest/download/install.sh \
  | MCP_RECALL_INSTALL_DIR="$HOME/bin" sh
```

Install without creating or updating configuration:

```bash
curl -fsSL https://github.com/maccavelli/mcp-server-recall/releases/latest/download/install.sh \
  | MCP_RECALL_NO_CONFIGURE=1 sh
```

Disable datastore encryption during configuration only when that is an
intentional local-security decision:

```bash
curl -fsSL https://github.com/maccavelli/mcp-server-recall/releases/latest/download/install.sh \
  | MCP_RECALL_ENCRYPT_DB=false sh
```

For `--dry-run`, `--verbose`, `--uninstall`, or the complete help text,
download the script before invoking it:

```bash
curl -fsSL -o /tmp/mcp-server-recall-install.sh \
  https://github.com/maccavelli/mcp-server-recall/releases/latest/download/install.sh
sh /tmp/mcp-server-recall-install.sh --dry-run
sh /tmp/mcp-server-recall-install.sh --help
```

### Windows options

The one-line PowerShell form uses defaults. To pass parameters, load the
published script as a script block:

```powershell
$url = 'https://github.com/maccavelli/mcp-server-recall/releases/latest/download/install.ps1'
$installer = [scriptblock]::Create((Invoke-RestMethod $url))
& $installer -Version 1.1.0 `
  -InstallDir (Join-Path $env:LOCALAPPDATA 'Programs\mcp-server-recall') `
  -EncryptDb true
```

Useful parameters and environment values:

| Setting | Effect |
|---|---|
| `-Version 1.1.0` | Download from the specified `vX.Y.Z` release instead of `latest`. |
| `-InstallDir PATH` | Override the per-user program directory. |
| `-EncryptDb false` | Configure without datastore encryption. |
| `-NoConfigure` | Install the binary without running `configure`. |
| `-WhatIf` | Show the intended source, target, and configuration action without downloading or writing. |
| `$env:MCP_RECALL_ENCRYPT_DB` | Set the default encryption argument. |
| `$env:MCP_RECALL_NO_CONFIGURE=1` | Skip configuration in the one-line form. |

## Requirements

The bootstrap installers require HTTPS access to GitHub Releases.

Linux and macOS require:

- POSIX `sh`;
- `curl` or `wget`;
- `awk` and `grep`;
- `sha256sum`, `shasum`, or `openssl`;
- `uname` and `mktemp`.

Windows requires PowerShell 5.1 or later and uses `Invoke-WebRequest` plus
`Get-FileHash`.

Install the Go version declared by [`go.mod`](../../go.mod) when building from
source. Current packages are available from the
[official Go downloads page](https://go.dev/dl/).

## Linux walkthrough

### Install and expose the binary

Run the recommended one-line installer. If it reports that
`$HOME/.local/bin` is not on PATH, add it for the current shell:

```bash
export PATH="$PATH:$HOME/.local/bin"
```

Persist the same line in the startup file used by your shell, such as
`~/.profile`, `~/.bashrc`, or `~/.zshrc`.

### Manual verified download

This fallback supports both release architectures and deliberately verifies by
hash value. The manifest contains versioned filenames even though the download
uses an unversioned alias.

```bash
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

mkdir -p "$HOME/.local/bin"
curl -fL -o /tmp/mcp-server-recall \
  "https://github.com/maccavelli/mcp-server-recall/releases/latest/download/mcp-server-recall-linux-${ARCH}"
curl -fL -o /tmp/mcp-server-recall-SHA256SUMS \
  https://github.com/maccavelli/mcp-server-recall/releases/latest/download/SHA256SUMS

EXPECTED=$(awk -v prefix="mcp-server-recall-linux-${ARCH}-" \
  '$2 ~ ("^" prefix "[0-9]") { print $1; exit }' \
  /tmp/mcp-server-recall-SHA256SUMS)
ACTUAL=$(sha256sum /tmp/mcp-server-recall | awk '{print $1}')
test -n "$EXPECTED" && test "$ACTUAL" = "$EXPECTED"
install -m 0755 /tmp/mcp-server-recall "$HOME/.local/bin/mcp-server-recall"
mcp-server-recall configure --encrypt-db=true
```

### Linux data locations

| Data | Default path |
|---|---|
| Configuration | `${XDG_CONFIG_HOME:-$HOME/.config}/mcp-server-recall/recall.yaml` |
| Datastore and Bleve index | `${XDG_DATA_HOME:-$HOME/.local/share}/mcp-server-recall/.mcp_recall` |
| Crash log | `${XDG_CACHE_HOME:-$HOME/.cache}/mcp-server-recall/crash.log` |

## macOS walkthrough

### Supported hardware

The release and installer support Apple silicon (`arm64`). The project does not
publish an Intel (`amd64`) macOS binary. Intel users can build locally with Go,
but that target is not part of project release CI.

### Install and expose the binary

Run the recommended one-line installer. It attempts to remove the downloaded
binary's quarantine attribute when `xattr` is available. If needed, add the
default directory to PATH:

```bash
export PATH="$PATH:$HOME/.local/bin"
```

### Manual verified download

```bash
mkdir -p "$HOME/.local/bin"
curl -fL -o /tmp/mcp-server-recall \
  https://github.com/maccavelli/mcp-server-recall/releases/latest/download/mcp-server-recall-darwin-arm64
curl -fL -o /tmp/mcp-server-recall-SHA256SUMS \
  https://github.com/maccavelli/mcp-server-recall/releases/latest/download/SHA256SUMS

EXPECTED=$(awk \
  '$2 ~ /^mcp-server-recall-darwin-arm64-[0-9]/ { print $1; exit }' \
  /tmp/mcp-server-recall-SHA256SUMS)
ACTUAL=$(shasum -a 256 /tmp/mcp-server-recall | awk '{print $1}')
test -n "$EXPECTED" && test "$ACTUAL" = "$EXPECTED"
install -m 0755 /tmp/mcp-server-recall "$HOME/.local/bin/mcp-server-recall"
xattr -d com.apple.quarantine "$HOME/.local/bin/mcp-server-recall" 2>/dev/null || true
mcp-server-recall configure --encrypt-db=true
```

### macOS data locations

| Data | Default path |
|---|---|
| Configuration | `$HOME/Library/Application Support/mcp-server-recall/recall.yaml` |
| Datastore and Bleve index | `$HOME/Library/Application Support/mcp-server-recall/.mcp_recall` |
| Crash log | `$HOME/Library/Caches/mcp-server-recall/crash.log` |

## Windows walkthrough

### Supported hardware

The release and installer support Windows amd64. The installer intentionally
refuses Windows arm64 rather than silently selecting an emulated amd64 binary.
The project does not publish or run CI against a native Windows arm64 binary.

### Install and expose the binary

Run the recommended PowerShell one-liner. The default binary is installed at:

```text
%LOCALAPPDATA%\Programs\mcp-server-recall\mcp-server-recall.exe
```

If the installer reports that the directory is missing from PATH, add it to the
user PATH while preserving the existing value:

```powershell
$installDir = Join-Path $env:LOCALAPPDATA 'Programs\mcp-server-recall'
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$entries = @($userPath -split ';' | Where-Object { $_ })
if ($entries -notcontains $installDir) {
  [Environment]::SetEnvironmentVariable(
    'Path',
    (($entries + $installDir) -join ';'),
    'User'
  )
}
```

Open a new terminal after changing PATH.

### Manual verified download

The release manifest uses the versioned name
`mcp-server-recall-windows-amd64-X.Y.Z.exe`; verification therefore selects
that prefix and compares its hash to the unversioned alias download.

```powershell
$download = Join-Path $env:TEMP 'mcp-server-recall.exe'
$sumsPath = Join-Path $env:TEMP 'mcp-server-recall-SHA256SUMS'
$installDir = Join-Path $env:LOCALAPPDATA 'Programs\mcp-server-recall'

Invoke-WebRequest `
  'https://github.com/maccavelli/mcp-server-recall/releases/latest/download/mcp-server-recall-windows-amd64.exe' `
  -OutFile $download
Invoke-WebRequest `
  'https://github.com/maccavelli/mcp-server-recall/releases/latest/download/SHA256SUMS' `
  -OutFile $sumsPath

$prefix = 'mcp-server-recall-windows-amd64-'
$line = Get-Content $sumsPath |
  Where-Object { $_ -match [regex]::Escape($prefix) } |
  Select-Object -First 1
if (-not $line) { throw "No checksum entry found for $prefix" }

$expected = (($line -split '\s+' | Where-Object { $_ })[0]).ToLower()
$actual = (Get-FileHash $download -Algorithm SHA256).Hash.ToLower()
if ($actual -ne $expected) {
  throw "SHA-256 mismatch: expected $expected, got $actual"
}

New-Item -ItemType Directory -Force -Path $installDir | Out-Null
Move-Item -Force $download (Join-Path $installDir 'mcp-server-recall.exe')
& (Join-Path $installDir 'mcp-server-recall.exe') configure --encrypt-db=true
```

### Windows data locations

| Data | Default path |
|---|---|
| Configuration | `%APPDATA%\mcp-server-recall\recall.yaml` |
| Datastore and Bleve index | `%LOCALAPPDATA%\mcp-server-recall\.mcp_recall` |
| Crash log | `%LOCALAPPDATA%\mcp-server-recall\crash.log` |

## Build current source

Clone the repository and use the Go version declared by `go.mod`.

Linux or macOS:

```bash
git clone https://github.com/maccavelli/mcp-server-recall.git
cd mcp-server-recall
mkdir -p "$HOME/.local/bin"
go build -trimpath -o "$HOME/.local/bin/mcp-server-recall" ./cmd/mcp-server-recall
"$HOME/.local/bin/mcp-server-recall" configure --encrypt-db=true
```

Windows PowerShell:

```powershell
git clone https://github.com/maccavelli/mcp-server-recall.git
Set-Location mcp-server-recall
$installDir = Join-Path $env:LOCALAPPDATA 'Programs\mcp-server-recall'
New-Item -ItemType Directory -Force -Path $installDir | Out-Null
go build -trimpath `
  -o (Join-Path $installDir 'mcp-server-recall.exe') `
  ./cmd/mcp-server-recall
& (Join-Path $installDir 'mcp-server-recall.exe') configure --encrypt-db=true
```

An ordinary `go build` uses the source fallback version. Project release builds
use the Makefile's `VERSION` linker setting to stamp the tag.

## Upgrade, pin, or remove

Re-run the one-line installer to upgrade to the latest release. Existing Unix
and Windows binaries are moved to a `.prev` file before replacement.

Pinning is performed by release version, without the leading `v`:

```bash
curl -fsSL https://github.com/maccavelli/mcp-server-recall/releases/latest/download/install.sh \
  | MCP_RECALL_VERSION=1.1.0 sh
```

The Unix installer supports removal from its selected install directory:

```bash
curl -fsSL -o /tmp/mcp-server-recall-install.sh \
  https://github.com/maccavelli/mcp-server-recall/releases/latest/download/install.sh
sh /tmp/mcp-server-recall-install.sh --uninstall
```

Uninstall removes the binary and its `.prev` sibling. It intentionally leaves
configuration, data, index, and log files in place. The PowerShell installer
does not currently implement uninstall; stop the server and remove only its
explicit program directory if removal is intended.

## Verify the installation

Open a new terminal if PATH was changed, then run:

```text
mcp-server-recall --version
mcp-server-recall --help
```

The installer configures the datastore by default. If configuration was
skipped, run:

```text
mcp-server-recall configure --encrypt-db=true
```

Start the server in the foreground for a standalone smoke test:

```text
mcp-server-recall serve
```

Normally an MCP client launches `serve` over stdio. Continue with
[Client integration](client-integration.md) or the
[standalone local-service walkthrough](getting-started.md#standalone-local-service).

## Troubleshooting

| Symptom | Explanation and action |
|---|---|
| `mcp-server-recall: command not found` | Add the installer destination to PATH and open a new terminal. GUI clients should use the absolute binary path. |
| Installer reports no checksum entry | Confirm the selected architecture is published and the requested release contains both its binary and `SHA256SUMS`. |
| Checksum mismatch | Nothing is installed. Retry on a trusted network; do not bypass verification. |
| Linux installer rejects the architecture | Only amd64 and arm64 are published; 32-bit ARM and other architectures are unsupported. |
| macOS installer rejects `x86_64` | No Intel macOS release binary is published. Build from source with a compatible Go toolchain. |
| Windows installer rejects ARM64 | No native Windows arm64 asset is published. The installer does not silently choose emulation. |
| Port 47669 is already in use | Set the same `MCP_ENDPOINT_API_PORT` for `serve` and administrative CLI processes, or set `MCP_REC_URL` for the CLI. |
