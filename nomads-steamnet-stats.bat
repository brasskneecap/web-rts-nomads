@echo off
REM nomads-steamnet-stats.bat
REM Prints the last 15 Steam Sockets transport stats lines from the Nomads
REM desktop shell log. Run on BOTH host and joiner machines while a laggy
REM match is in progress (or right after).
REM
REM Usage: double-click, or from a command prompt: nomads-steamnet-stats.bat

setlocal

set "LOGS_DIR=%APPDATA%\Nomads\logs"

if not exist "%LOGS_DIR%" (
    echo ERROR: %LOGS_DIR% not found. Launch the Nomads desktop app once, then re-run.
    pause
    exit /b 1
)

REM Find the newest *-shell.log file.
set "LATEST="
for /f "delims=" %%F in ('dir /b /o-d "%LOGS_DIR%\*-shell.log" 2^>nul') do (
    if not defined LATEST set "LATEST=%LOGS_DIR%\%%F"
)

if not defined LATEST (
    echo ERROR: no *-shell.log in %LOGS_DIR%
    pause
    exit /b 1
)

echo === machine: %COMPUTERNAME% ===
echo === file:    %LATEST% ===
echo === last 15 steam_net stats lines ===
echo.

REM Grep for "steam_net stats" lines and keep only the last 15.
powershell -NoProfile -Command "Select-String -Path '%LATEST%' -Pattern 'steam_net stats' | Select-Object -Last 15 | ForEach-Object { $_.Line }"

echo.
pause
