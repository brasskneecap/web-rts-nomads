# nomads-steamnet-stats.ps1
# Prints the last 15 Steam Sockets transport stats lines from the Nomads
# desktop shell log. Run this on BOTH the host and joiner machines while a
# laggy match is in progress (or right after).
#
# Usage:
#   powershell -ExecutionPolicy Bypass -File nomads-steamnet-stats.ps1
#
# If the file isn't found, make sure the desktop game has been launched at
# least once on this machine so the logs/ folder exists.

$LogsDir = Join-Path $env:APPDATA "Nomads\logs"

if (-not (Test-Path $LogsDir)) {
    Write-Host "ERROR: logs directory not found at: $LogsDir" -ForegroundColor Red
    Write-Host "Launch the Nomads desktop app once, then re-run this script."
    exit 1
}

$Latest = Get-ChildItem -Path $LogsDir -Filter "*-shell.log" `
    | Sort-Object LastWriteTime -Descending `
    | Select-Object -First 1

if ($null -eq $Latest) {
    Write-Host "ERROR: no *-shell.log file in $LogsDir" -ForegroundColor Red
    exit 1
}

Write-Host "=== machine: $env:COMPUTERNAME ==="
Write-Host "=== file:    $($Latest.FullName) ==="
Write-Host "=== last 15 steam_net stats lines ==="
Write-Host ""

Select-String -Path $Latest.FullName -Pattern "steam_net stats" `
    | Select-Object -Last 15 `
    | ForEach-Object { $_.Line }
