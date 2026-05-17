# Nomads desktop build pipeline — PowerShell equivalent of Makefile.
#
# Use this on Windows where `make` is typically not installed. macOS/Linux can
# use the Makefile; both produce identical artefacts.
#
# Usage:
#   .\build.ps1                # shows help
#   .\build.ps1 -Target spa
#   .\build.ps1 -Target sidecar
#   .\build.ps1 -Target package
#   .\build.ps1 -Target test
#   $env:NOMADS_VERSION="v0.1.0"; .\build.ps1 -Target package

[CmdletBinding()]
param(
    [ValidateSet('help', 'spa', 'stage-dist', 'server', 'sidecar', 'shell', 'package', 'test', 'test-go', 'test-rust', 'clean')]
    [string]$Target = 'help'
)

$ErrorActionPreference = 'Stop'
$RepoRoot = $PSScriptRoot

# --- Self-heal PATH so shells that started before rustup installed still work
# rustup installs to %USERPROFILE%\.cargo\bin and adds that to the user PATH,
# but the change doesn't propagate to already-running shells. Prepend it here
# if it's not already on PATH so freshly-opened sessions don't need to be
# restarted just to run cargo/rustc.
$CargoBin = Join-Path $env:USERPROFILE '.cargo\bin'
if ((Test-Path $CargoBin) -and ($env:PATH -notlike "*$CargoBin*")) {
    $env:PATH = "$CargoBin;$env:PATH"
}

# --- Resolve build inputs ----------------------------------------------------

function Get-Version {
    if ($env:NOMADS_VERSION -and $env:NOMADS_VERSION.Length -gt 0) {
        return $env:NOMADS_VERSION
    }
    try {
        $sha = (git rev-parse --short HEAD 2>$null).Trim()
        if ($sha) { return $sha }
    } catch {}
    return 'unknown'
}

function Get-RustcHost {
    $info = & rustc -vV
    foreach ($line in $info) {
        if ($line -match '^host:\s+(.+)$') { return $Matches[1] }
    }
    throw "could not determine rustc host triple"
}

$Version = Get-Version

# Paths
$SpaDir       = Join-Path $RepoRoot 'client\src\game-portal'
$SpaDist      = Join-Path $SpaDir 'dist'
$EmbedDist    = Join-Path $RepoRoot 'server\cmd\api\dist'
$SidecarDir   = Join-Path $RepoRoot 'desktop\src-tauri\binaries'

# --- Targets -----------------------------------------------------------------

function Invoke-Help {
    Write-Host ""
    Write-Host "Nomads build pipeline (PowerShell)"
    Write-Host ""
    Write-Host "Targets:"
    Write-Host "  spa         - npm run build (Vue SPA)"
    Write-Host "  stage-dist  - copy SPA dist into server module"
    Write-Host "  server      - build Go server (no embed)"
    Write-Host "  sidecar     - go build -tags embed_spa into desktop sidecar slot"
    Write-Host "  shell       - cargo tauri build (requires sidecar staged)"
    Write-Host "  package     - full pipeline: spa -> stage-dist -> sidecar -> shell"
    Write-Host "  test        - run all Go + Rust tests"
    Write-Host "  test-go     - go test ./..."
    Write-Host "  test-rust   - cargo test --lib"
    Write-Host "  clean       - remove built artefacts"
    Write-Host ""
    Write-Host "Version:     $Version  (override with `$env:NOMADS_VERSION='...')"
    try {
        Write-Host "Host triple: $(Get-RustcHost)"
    } catch {
        Write-Host "Host triple: (rustc not found on PATH; install rustup first)"
    }
}

function Invoke-Spa {
    Push-Location $SpaDir
    try {
        $env:NOMADS_VERSION = $Version
        & npm run build
        if ($LASTEXITCODE -ne 0) { throw "npm run build failed (exit $LASTEXITCODE)" }
    } finally {
        Pop-Location
    }
}

function Invoke-StageDist {
    Invoke-Spa
    if (Test-Path $EmbedDist) {
        Remove-Item -Recurse -Force $EmbedDist
    }
    Copy-Item -Recurse $SpaDist $EmbedDist
    Write-Host "staged $SpaDist -> $EmbedDist"
}

function Invoke-Server {
    Push-Location (Join-Path $RepoRoot 'server')
    try {
        $binDir = Join-Path $RepoRoot 'bin'
        if (-not (Test-Path $binDir)) { New-Item -ItemType Directory -Path $binDir | Out-Null }
        & go build -ldflags "-X main.version=$Version" -o (Join-Path $binDir 'api.exe') ./cmd/api
        if ($LASTEXITCODE -ne 0) { throw "go build failed (exit $LASTEXITCODE)" }
    } finally {
        Pop-Location
    }
}

function Invoke-Sidecar {
    Invoke-StageDist
    if (-not (Test-Path $SidecarDir)) { New-Item -ItemType Directory -Path $SidecarDir | Out-Null }
    $triple  = Get-RustcHost
    $ext     = if ($IsWindows -or $env:OS -match 'Windows') { '.exe' } else { '' }
    $outPath = Join-Path $SidecarDir "nomads-server-$triple$ext"
    Push-Location (Join-Path $RepoRoot 'server')
    try {
        & go build -tags embed_spa -ldflags "-X main.version=$Version" -o $outPath ./cmd/api
        if ($LASTEXITCODE -ne 0) { throw "go build -tags embed_spa failed (exit $LASTEXITCODE)" }
        Write-Host "sidecar built: $outPath"
    } finally {
        Pop-Location
    }
}

function Invoke-Shell {
    Invoke-Sidecar
    Push-Location (Join-Path $RepoRoot 'desktop\src-tauri')
    try {
        & cargo tauri build
        if ($LASTEXITCODE -ne 0) { throw "cargo tauri build failed (exit $LASTEXITCODE)" }
    } finally {
        Pop-Location
    }
}

function Invoke-Package {
    Invoke-Shell
    Write-Host ""
    Write-Host "Packaging complete. Artefacts:"
    $targetDir = Join-Path $RepoRoot 'desktop\src-tauri\target'
    if (Test-Path $targetDir) {
        Get-ChildItem -Recurse -Path $targetDir -Include *.msi, *.dmg, *.AppImage, *.deb -ErrorAction SilentlyContinue |
            ForEach-Object { Write-Host "  $($_.FullName)" }
    }
}

function Invoke-TestGo {
    Push-Location (Join-Path $RepoRoot 'server')
    try {
        & go test -count=1 ./...
        if ($LASTEXITCODE -ne 0) { throw "go test failed (exit $LASTEXITCODE)" }
    } finally {
        Pop-Location
    }
}

function Invoke-TestRust {
    Push-Location (Join-Path $RepoRoot 'desktop\src-tauri')
    try {
        & cargo test --lib
        if ($LASTEXITCODE -ne 0) { throw "cargo test failed (exit $LASTEXITCODE)" }
    } finally {
        Pop-Location
    }
}

function Invoke-Test {
    Invoke-TestGo
    Invoke-TestRust
}

function Invoke-Clean {
    foreach ($p in @(
        (Join-Path $RepoRoot 'bin'),
        $EmbedDist,
        $SidecarDir,
        (Join-Path $RepoRoot 'desktop\src-tauri\target'),
        $SpaDist
    )) {
        if (Test-Path $p) {
            Remove-Item -Recurse -Force $p
            Write-Host "removed $p"
        }
    }
}

# --- Dispatch ----------------------------------------------------------------

switch ($Target) {
    'help'        { Invoke-Help }
    'spa'         { Invoke-Spa }
    'stage-dist'  { Invoke-StageDist }
    'server'      { Invoke-Server }
    'sidecar'     { Invoke-Sidecar }
    'shell'       { Invoke-Shell }
    'package'     { Invoke-Package }
    'test'        { Invoke-Test }
    'test-go'     { Invoke-TestGo }
    'test-rust'   { Invoke-TestRust }
    'clean'       { Invoke-Clean }
}
