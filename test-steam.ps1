# Atomic smoke test for Steam-feature builds. Rebuilds → verifies binary
# fingerprint → launches → tails the log. Use this instead of separate
# build / launch / read-log steps so nothing can sneak in and overwrite the
# binary between verification and launch.

$ErrorActionPreference = 'Stop'
$RepoRoot = $PSScriptRoot
$Exe = Join-Path $RepoRoot 'desktop\src-tauri\target\release\nomads-desktop.exe'

# Make sure cargo is on PATH even in fresh shells.
$CargoBin = Join-Path $env:USERPROFILE '.cargo\bin'
if ((Test-Path $CargoBin) -and ($env:PATH -notlike "*$CargoBin*")) {
    $env:PATH = "$CargoBin;$env:PATH"
}

Write-Host "Step 1/5 — rebuild Steam-feature binary"
& (Join-Path $RepoRoot 'build.ps1') -Target shell -Steam
if ($LASTEXITCODE -ne 0) { throw "build failed" }

Write-Host ""
Write-Host "Step 2/5 — verify binary is Steam-linked"
if (-not (Test-Path $Exe)) { throw "binary missing at $Exe" }
$bytes = [System.IO.File]::ReadAllBytes($Exe)
$text = [System.Text.Encoding]::ASCII.GetString($bytes)
# A Steam build imports steam_api64.dll; a non-Steam build has zero references.
# (This replaces an older NOMADS_BUILD_VARIANT::STEAM_BUILD_V2 marker string that
# was removed from the Rust source — the raw import is the reliable signal.)
$steamRefs = ([regex]::Matches($text, 'steam_api64')).Count
Write-Host "  mtime:        $((Get-Item $Exe).LastWriteTime)"
Write-Host "  size:         $((Get-Item $Exe).Length) bytes"
Write-Host "  steam_api64:  $steamRefs references"
if ($steamRefs -lt 1) {
    throw "binary is NOT a Steam build (no steam_api64 references). Aborting before launch."
}

Write-Host ""
Write-Host "Step 3/5 — launch"
Start-Process $Exe

Write-Host ""
Write-Host "Step 4/5 — waiting 6s for startup..."
Start-Sleep -Seconds 6

Write-Host ""
Write-Host "Step 5/5 — latest shell log"
$log = Get-ChildItem ($env:APPDATA + '\Nomads\logs\*-shell.log') |
    Sort-Object LastWriteTime -Descending |
    Select-Object -First 1
Write-Host "  --- $($log.Name) ---"
Get-Content $log.FullName
