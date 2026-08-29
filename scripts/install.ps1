#Requires -Version 5.1
<#
.SYNOPSIS
    Install mcp-server-recall on Windows.

.DESCRIPTION
    Downloads the published windows/amd64 binary, verifies it against the
    release SHA256SUMS manifest by hash VALUE (not filename), and installs
    under %LOCALAPPDATA%\Programs\mcp-server-recall — per-user, no elevation.

    Mirrors scripts/install.sh (docs/0003-MADR-curl-bootstrap-installer.md).

.PARAMETER Version
    Install a specific release (e.g. 1.0.3) instead of the latest.

.PARAMETER InstallDir
    Override the install directory.

.PARAMETER WhatIf
    Show what would happen without downloading or installing anything.
#>
[CmdletBinding(SupportsShouldProcess = $true)]
param(
    [string]$Version,
    [string]$InstallDir,
    [string]$EncryptDb = $(if ($env:MCP_RECALL_ENCRYPT_DB) { $env:MCP_RECALL_ENCRYPT_DB } else { 'true' }),
    [switch]$NoConfigure,
    [string]$BaseUrl = 'https://github.com/maccavelli/mcp-server-recall/releases'
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$Product = 'mcp-server-recall'

function Write-Log { param([string]$Message) Write-Host "install: $Message" }
function Write-Warn { param([string]$Message) Write-Warning "install: $Message" }

function Get-TargetArch {
    $arch = $env:PROCESSOR_ARCHITECTURE
    if ($env:PROCESSOR_ARCHITEW6432) { $arch = $env:PROCESSOR_ARCHITEW6432 }
    switch ($arch) {
        'AMD64' { return 'amd64' }
        'ARM64' {
            throw @'
windows/arm64 is not a published target.
An amd64 build will run under emulation on Windows on Arm, but this installer
does not select it silently. Download mcp-server-recall-windows-amd64.exe by hand if
that is what you want.
'@
        }
        default { throw "unsupported processor architecture '$arch'; only amd64 is published." }
    }
}

function Get-UrlDir {
    if ($Version) { return "$BaseUrl/download/v$Version" }
    return "$BaseUrl/latest/download"
}

function Get-File {
    param([string]$Url, [string]$Destination)
    Write-Verbose "fetching $Url"
    $previous = $ProgressPreference
    $ProgressPreference = 'SilentlyContinue'
    try {
        Invoke-WebRequest -Uri $Url -OutFile $Destination -UseBasicParsing
    } finally {
        $ProgressPreference = $previous
    }
}

function Resolve-Product {
    param(
        [string]$Arch,
        [string]$BinaryPath,
        [string[]]$Sums
    )
    $prefix = "$Product-windows-$Arch-"
    $line = $Sums | Where-Object { $_ -match [regex]::Escape($prefix) } | Select-Object -First 1
    if (-not $line) {
        throw "no checksum entry for $prefix* in SHA256SUMS"
    }
    $fields = $line -split '\s+' | Where-Object { $_ }
    $want = $fields[0]
    $name = $fields[-1]

    $got = (Get-FileHash -Path $BinaryPath -Algorithm SHA256).Hash.ToLower()
    if ($want.ToLower() -ne $got) {
        throw @"
checksum mismatch for $Product
  expected $($want.ToLower())
  got      $got
Nothing was installed.
"@
    }
    $resolved = $name.Substring($prefix.Length)
    if ($resolved.EndsWith('.exe')) {
        $resolved = $resolved.Substring(0, $resolved.Length - 4)
    }
    return $resolved
}

function Add-ToPathNotice {
    param([string]$Dir)
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if ($userPath -and ($userPath -split ';' | Where-Object { $_.TrimEnd('\') -ieq $Dir.TrimEnd('\') })) {
        return
    }
    Write-Warn "$Dir is not on your PATH. To add it for this user:"
    Write-Host "  [Environment]::SetEnvironmentVariable('Path', `"`$env:Path;$Dir`", 'User')"
}

$arch = Get-TargetArch
$urlDir = Get-UrlDir
if (-not $InstallDir) {
    $InstallDir = Join-Path $env:LOCALAPPDATA "Programs\$Product"
}
Write-Log "source $urlDir"
Write-Log "target windows/$arch -> $InstallDir"

$encryptNorm = $EncryptDb.Trim().ToLower()
switch ($encryptNorm) {
    { $_ -in @('true', 'yes', '1') } { $encryptNorm = 'true' }
    { $_ -in @('false', 'no', '0') } { $encryptNorm = 'false' }
    default { throw "--encrypt-db must be true or false (got $EncryptDb)" }
}
if ($env:MCP_RECALL_NO_CONFIGURE -eq '1') { $NoConfigure = $true }
Write-Log "configure --encrypt-db=$encryptNorm"

if ($WhatIfPreference) {
    Write-Log "would download $urlDir/$Product-windows-$arch.exe"
    Write-Log "would install  $(Join-Path $InstallDir "$Product.exe")"
    if ($NoConfigure) { Write-Log 'would skip configure' }
    else { Write-Log "would run $Product configure --encrypt-db=$encryptNorm" }
    Write-Log 'nothing was downloaded (-WhatIf)'
    return
}

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("recall-install-" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp -Force | Out-Null
try {
    $sumsPath = Join-Path $tmp 'SHA256SUMS'
    try {
        Get-File -Url "$urlDir/SHA256SUMS" -Destination $sumsPath
    } catch {
        throw @"
could not download SHA256SUMS from $urlDir
If you pinned a version, check that the release exists and carries the
unversioned alias assets.
"@
    }
    $sums = Get-Content -Path $sumsPath

    $dl = Join-Path $tmp "$Product.exe"
    Get-File -Url "$urlDir/$Product-windows-$arch.exe" -Destination $dl
    $resolvedVersion = Resolve-Product -Arch $arch -BinaryPath $dl -Sums $sums
    Write-Log "$Product verified, version $resolvedVersion"

    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    $target = Join-Path $InstallDir "$Product.exe"
    if ($PSCmdlet.ShouldProcess($target, 'install')) {
        if (Test-Path $target) {
            $backup = "$target.prev"
            Remove-Item -Path $backup -Force -ErrorAction SilentlyContinue
            Move-Item -Path $target -Destination $backup -Force
        }
        Move-Item -Path $dl -Destination $target -Force
        Write-Log "installed $target"
    }

    if (Test-Path $target) {
        $reported = (& $target --version 2>&1 | Out-String).Trim() -split '\s+' | Select-Object -Last 1
        if ($resolvedVersion -and $reported -and ($reported -ne $resolvedVersion)) {
            Write-Warn "installed binary reports '$reported' but the manifest said '$resolvedVersion'"
        }
    }

    Add-ToPathNotice -Dir $InstallDir
    if (-not $NoConfigure) {
        Write-Log "running $Product configure --encrypt-db=$encryptNorm"
        & $target configure --encrypt-db=$encryptNorm
        if ($LASTEXITCODE -ne 0) {
            throw "configure --encrypt-db=$encryptNorm failed with exit $LASTEXITCODE"
        }
    } else {
        Write-Log "configure skipped (-NoConfigure)"
    }
    Write-Host ''
    Write-Log "installed $Product"
} finally {
    Remove-Item -Path $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
