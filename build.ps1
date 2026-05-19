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
    [string]$Target = 'help',
    # Enable the Steamworks SDK integration. Requires STEAM_SDK_LOCATION env
    # var and desktop/src-tauri/steam_appid.txt — see desktop/README.md.
    [switch]$Steam
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
    Write-Host "Flags:"
    Write-Host "  -Steam      - enable Steamworks integration (requires STEAM_SDK_LOCATION)"
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
    # Windows will fail with "Access is denied" if nomads-desktop.exe is
    # currently running (we can't overwrite a locked binary). Kill any
    # lingering instances first so the build doesn't blow up halfway through
    # the link step.
    Get-Process -Name 'nomads-desktop' -ErrorAction SilentlyContinue | ForEach-Object {
        Write-Host "shell: stopping running nomads-desktop.exe (pid $($_.Id)) to free the binary"
        $_ | Stop-Process -Force
    }
    # Also kill any orphaned sidecar children (would hold open log files we
    # don't strictly need to overwrite, but they're noisy if they linger).
    Get-Process -Name 'nomads-server' -ErrorAction SilentlyContinue | ForEach-Object {
        Write-Host "shell: stopping orphaned nomads-server.exe (pid $($_.Id))"
        $_ | Stop-Process -Force
    }
    Push-Location (Join-Path $RepoRoot 'desktop\src-tauri')
    try {
        if ($Steam) {
            # cargo tauri build silently rebuilds the binary without the
            # feature flag (cargo-tauri CLI bug — --features doesn't always
            # propagate). Skip the bundle step entirely for the Steam path
            # so the binary at target/release/nomads-desktop.exe is the one
            # cargo build emitted, not whatever cargo tauri build replaced
            # it with. MSI bundling for Steam builds is a follow-up.
            $cargoArgs = @('build', '--release', '--features', 'steam')
            Write-Host "shell: building with Steamworks (--features steam, no bundle)"
            Write-Host "shell: invoking: cargo $($cargoArgs -join ' ')"
            & cargo @cargoArgs
            if ($LASTEXITCODE -ne 0) { throw "cargo build failed (exit $LASTEXITCODE)" }
        }
        else {
            # No-Steam path: keep the original cargo tauri build (produces
            # the MSI installer + bundled artefacts).
            $tauriArgs = @('tauri', 'build')
            Write-Host "shell: invoking: cargo $($tauriArgs -join ' ')"
            & cargo @tauriArgs
            if ($LASTEXITCODE -ne 0) { throw "cargo tauri build failed (exit $LASTEXITCODE)" }
        }

        if ($Steam) {
            # Stage steam_api64.dll next to the raw nomads-desktop.exe so the
            # user can launch target/release/nomads-desktop.exe directly. The
            # steamworks-sys crate extracts the DLL into a build-script OUT_DIR;
            # find the latest copy and place it where Windows can load it.
            $releaseDir = Join-Path $RepoRoot 'desktop\src-tauri\target\release'
            $dll = Get-ChildItem -Path (Join-Path $releaseDir 'build') -Recurse -Filter 'steam_api64.dll' -ErrorAction SilentlyContinue |
                Sort-Object LastWriteTime -Descending |
                Select-Object -First 1
            if ($null -eq $dll) {
                Write-Warning "steam_api64.dll not found in target/release/build/; raw .exe launch may fail at runtime"
            }
            else {
                Copy-Item $dll.FullName (Join-Path $releaseDir 'steam_api64.dll') -Force
                Write-Host "staged steam_api64.dll next to nomads-desktop.exe"
            }

            # Stage steam_appid.txt next to the raw .exe so
            # SteamAPI_RestartAppIfNecessary returns false in dev mode. Without
            # this, launching nomads-desktop.exe directly causes Steam to
            # relaunch us as "appid 480" — which Steam knows as Spacewar, so
            # it launches the actual Spacewar binary instead of ours.
            #
            # The source file is gitignored (it's a dev-only artefact that
            # MUST NOT be in release bundles per Steam SDK rules), so a
            # fresh clone won't have it. Auto-create with the Spacewar
            # appid when missing so the build is self-contained for any
            # developer running -Steam. When the project moves to a real
            # paid appid in Phase 3, this auto-create stays as the
            # development-against-Spacewar path and the real appid lives
            # in lib.rs's STEAM_APPID constant.
            $appidSrc = Join-Path $RepoRoot 'desktop\src-tauri\steam_appid.txt'
            if (-not (Test-Path $appidSrc)) {
                Write-Host "desktop/src-tauri/steam_appid.txt missing; auto-creating with appid 480 (Spacewar, dev)"
                Set-Content -Path $appidSrc -Value '480' -NoNewline -Encoding ASCII
            }
            Copy-Item $appidSrc (Join-Path $releaseDir 'steam_appid.txt') -Force
            Write-Host "staged steam_appid.txt next to nomads-desktop.exe"
        }
    } finally {
        Pop-Location
    }
}

function Invoke-Package {
    Invoke-Shell
    Write-Host ""
    if ($Steam) {
        # Steam path: we skipped cargo tauri build to avoid overwriting the
        # Steam binary with a non-Steam one. MSI/DMG/AppImage are not produced.
        Write-Host "Steam build complete (raw .exe only, no MSI)."
        $releaseDir = Join-Path $RepoRoot 'desktop\src-tauri\target\release'
        Write-Host "Artefacts at $releaseDir :"
        foreach ($f in 'nomads-desktop.exe', 'steam_api64.dll', 'steam_appid.txt') {
            $path = Join-Path $releaseDir $f
            if (Test-Path $path) { Write-Host "  $path" }
        }
        Write-Host ""
        Write-Host "Launch: $releaseDir\nomads-desktop.exe"
    }
    else {
        Write-Host "Packaging complete. Artefacts:"
        $targetDir = Join-Path $RepoRoot 'desktop\src-tauri\target'
        if (Test-Path $targetDir) {
            Get-ChildItem -Recurse -Path $targetDir -Include *.msi, *.dmg, *.AppImage, *.deb -ErrorAction SilentlyContinue |
                ForEach-Object { Write-Host "  $($_.FullName)" }
        }
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
