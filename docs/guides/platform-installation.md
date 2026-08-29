# Platform installation

This guide covers the v1.1.0 release installers, manual verified downloads, and
current-source builds.

## Release versus source

The v1.1.0 release publishes:

- Linux amd64 and arm64 binaries;
- a macOS arm64 binary;
- a Windows amd64 binary;
- versioned and unversioned `SHA256SUMS` manifests;
- POSIX and PowerShell bootstrap installers.

Use the bootstrap installer for the shortest supported installation. Build from
source when you need unreleased `main` behavior. Manual verified downloads are
provided as a transparent fallback.

## One-line installation

### macOS or Linux

```bash
curl -fsSL https://github.com/maccavelli/mcp-server-recall/releases/latest/download/install.sh | sh
```

The POSIX installer defaults to `$HOME/.local/bin`, requires no `sudo`, verifies
SHA-256, clears macOS quarantine when possible, and configures an encrypted
datastore. Run a downloaded copy with `--help` for version pinning, a custom
directory, dry-run, configuration opt-out, and uninstall options.

### Windows PowerShell

```powershell
irm https://github.com/maccavelli/mcp-server-recall/releases/latest/download/install.ps1 | iex
```

The PowerShell installer defaults to
`%LOCALAPPDATA%\Programs\mcp-server-recall`, verifies SHA-256, and configures an
encrypted datastore without elevation. A downloaded copy supports `-Version`,
`-InstallDir`, `-NoConfigure`, and `-WhatIf`.

## Requirements

The prebuilt server binary has no Go or Cgo runtime dependency. Install Go when
either condition applies:

- you are building this repository; `go.mod` currently requires Go 1.26.5;
- you will use `harvest projects` or `harvest standards`.

Go 1.26.5 packages for all three operating systems are available from the
[official Go downloads page](https://go.dev/dl/).

## Linux

### Supported architectures

- amd64: current release and source builds.
- arm64: current release and source builds.

### Manual verified binary

```bash
ARCH=amd64 # use arm64 on 64-bit ARM Linux
mkdir -p "$HOME/.local/bin"
curl -fL -o /tmp/mcp-server-recall \
  "https://github.com/maccavelli/mcp-server-recall/releases/latest/download/mcp-server-recall-linux-${ARCH}"
curl -fL -o /tmp/SHA256SUMS \
  https://github.com/maccavelli/mcp-server-recall/releases/latest/download/SHA256SUMS
grep " mcp-server-recall-linux-${ARCH}$" /tmp/SHA256SUMS \
  | sed "s#mcp-server-recall-linux-${ARCH}#/tmp/mcp-server-recall#" \
  | sha256sum -c -
install -m 0755 /tmp/mcp-server-recall "$HOME/.local/bin/mcp-server-recall"
```

Add the install directory to PATH if needed:

```bash
export PATH="$PATH:$HOME/.local/bin"
```

Persist that line in the startup file for your shell.

### Build current source

```bash
git clone https://github.com/maccavelli/mcp-server-recall.git
cd mcp-server-recall
mkdir -p "$HOME/.local/bin"
go build -trimpath -o "$HOME/.local/bin/mcp-server-recall" ./cmd/mcp-server-recall
mcp-server-recall configure --encrypt-db=true
```

Default locations:

| Data | Linux path |
|---|---|
| Configuration | `${XDG_CONFIG_HOME:-$HOME/.config}/mcp-server-recall/recall.yaml` |
| Datastore and index | `${XDG_DATA_HOME:-$HOME/.local/share}/mcp-server-recall/.mcp_recall` |
| Crash log | `${XDG_CACHE_HOME:-$HOME/.cache}/mcp-server-recall/crash.log` |

If an MCP client cannot find Go, use the result of `command -v go` as
`MCP_GO_BIN_PATH`.

## macOS

### Supported architectures

The project publishes Apple-silicon (`darwin-arm64`) builds. It does not publish
an Intel (`darwin-amd64`) binary, even though the Go toolchain itself supports
Intel macOS.

### Manual verified Apple-silicon binary

```bash
mkdir -p "$HOME/.local/bin"
curl -fL -o /tmp/mcp-server-recall \
  https://github.com/maccavelli/mcp-server-recall/releases/latest/download/mcp-server-recall-darwin-arm64
curl -fL -o /tmp/SHA256SUMS \
  https://github.com/maccavelli/mcp-server-recall/releases/latest/download/SHA256SUMS
grep ' mcp-server-recall-darwin-arm64$' /tmp/SHA256SUMS \
  | sed 's#mcp-server-recall-darwin-arm64#/tmp/mcp-server-recall#' \
  | shasum -a 256 -c -
install -m 0755 /tmp/mcp-server-recall "$HOME/.local/bin/mcp-server-recall"
xattr -d com.apple.quarantine "$HOME/.local/bin/mcp-server-recall" 2>/dev/null || true
```

Then add `$HOME/.local/bin` to PATH if it is not already present.

### Build current source

```bash
git clone https://github.com/maccavelli/mcp-server-recall.git
cd mcp-server-recall
mkdir -p "$HOME/.local/bin"
go build -trimpath -o "$HOME/.local/bin/mcp-server-recall" ./cmd/mcp-server-recall
mcp-server-recall configure --encrypt-db=true
```

Default locations:

| Data | macOS path |
|---|---|
| Configuration | `$HOME/Library/Application Support/mcp-server-recall/recall.yaml` |
| Datastore and index | `$HOME/Library/Application Support/mcp-server-recall/.mcp_recall` |
| Crash log | `$HOME/Library/Caches/mcp-server-recall/crash.log` |

The official Go `.pkg` normally installs `go` at `/usr/local/go/bin/go`. GUI
clients often have a smaller PATH than the terminal; set
`MCP_GO_BIN_PATH=/usr/local/go/bin/go` in the MCP client configuration when
harvesting fails only from the GUI.

## Windows

### Supported architectures

The project publishes Windows amd64. It does not publish Windows arm64; the
PowerShell installer on `main` intentionally refuses to select an amd64 binary
under emulation.

### Manual verified amd64 binary

Run PowerShell 5.1 or later:

```powershell
$download = Join-Path $env:TEMP 'mcp-server-recall.exe'
$sums = Join-Path $env:TEMP 'mcp-server-recall-SHA256SUMS'
$installDir = Join-Path $env:LOCALAPPDATA 'Programs\mcp-server-recall'

Invoke-WebRequest `
  'https://github.com/maccavelli/mcp-server-recall/releases/latest/download/mcp-server-recall-windows-amd64.exe' `
  -OutFile $download
Invoke-WebRequest `
  'https://github.com/maccavelli/mcp-server-recall/releases/latest/download/SHA256SUMS' `
  -OutFile $sums

$line = (Get-Content $sums | Select-String 'mcp-server-recall-windows-amd64.exe$').Line
$expected = ($line -split '\s+')[0].ToLower()
$actual = (Get-FileHash $download -Algorithm SHA256).Hash.ToLower()
if ($actual -ne $expected) { throw "SHA-256 mismatch: expected $expected, got $actual" }

New-Item -ItemType Directory -Force -Path $installDir | Out-Null
Move-Item -Force $download (Join-Path $installDir 'mcp-server-recall.exe')
```

Add the directory to the user PATH, preserving its existing value:

```powershell
$installDir = Join-Path $env:LOCALAPPDATA 'Programs\mcp-server-recall'
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($userPath -split ';') -notcontains $installDir) {
  [Environment]::SetEnvironmentVariable('Path', "$userPath;$installDir", 'User')
}
```

Open a new PowerShell window, then configure:

```powershell
mcp-server-recall.exe configure --encrypt-db=true
```

### Build current source

```powershell
git clone https://github.com/maccavelli/mcp-server-recall.git
Set-Location mcp-server-recall
$installDir = Join-Path $env:LOCALAPPDATA 'Programs\mcp-server-recall'
New-Item -ItemType Directory -Force -Path $installDir | Out-Null
go build -trimpath -o (Join-Path $installDir 'mcp-server-recall.exe') ./cmd/mcp-server-recall
& (Join-Path $installDir 'mcp-server-recall.exe') configure --encrypt-db=true
```

Default locations:

| Data | Windows path |
|---|---|
| Configuration | `%APPDATA%\mcp-server-recall\recall.yaml` |
| Datastore and index | `%LOCALAPPDATA%\mcp-server-recall\.mcp_recall` |
| Crash log | `%LOCALAPPDATA%\mcp-server-recall\crash.log` |

The official Go MSI normally places `go.exe` under
`C:\Program Files\Go\bin\go.exe`. Use that absolute path for
`MCP_GO_BIN_PATH` when a GUI client cannot inherit the system PATH.

## Configure and verify on every platform

```text
mcp-server-recall configure --encrypt-db=true
mcp-server-recall --version
mcp-server-recall --help
```

On PowerShell, append `.exe` if the program directory is not resolving through
PATHEXT/PATH.

Next, follow [Client integration](client-integration.md) for stdio MCP or
[Getting started](getting-started.md#standalone-local-service) for the local
administrative service.
