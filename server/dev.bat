@echo off
set GOPATH=%USERPROFILE%\go-workspace
set PATH=%PATH%;%USERPROFILE%\go\bin;%USERPROFILE%\go-workspace\bin
REM Dev backend port. Kept off Go's :8080 default because a Chromium/CEF
REM remote-debugger (e.g. steamwebhelper) squats :8080 and shadows the server,
REM which makes the Vite proxy's /maps, /catalog, ... calls 404. The Vite proxy
REM target in client/src/game-portal/vite.config.ts must match this.
set WEBRTS_PORT=8137
REM Tick-profile: prints per-section timings to stderr every ~5s and fires an
REM immediate [tick-profile][SLOW] line on any single section >= WEBRTS_TICK_SLOW_MS.
REM Disabled by default; flip WEBRTS_TICK_PROFILE=1 (and pick a slow-ms threshold,
REM e.g. 15) when investigating tick performance.
set WEBRTS_TICK_PROFILE=0
set WEBRTS_TICK_SLOW_MS=0
set WEBRTS_DEBUG_PATHING=0
set WEBRTS_SEND_PROFILE=1
cd /d "%~dp0"
REM `air` is the hot-reload runner; falls back to `go run` when not installed.
where air >nul 2>nul
if %errorlevel%==0 (
    air
) else (
    echo air not found on PATH; running `go run ./cmd/api` instead.
    go run ./cmd/api
)
