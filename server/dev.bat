@echo off
set GOPATH=%USERPROFILE%\go-workspace
set PATH=%PATH%;%USERPROFILE%\go\bin;%USERPROFILE%\go-workspace\bin
REM Tick-profile: prints per-section timings to stderr every ~5s and fires an
REM immediate [tick-profile][SLOW] line on any single section >= WEBRTS_TICK_SLOW_MS.
REM Disabled by default; flip WEBRTS_TICK_PROFILE=1 (and pick a slow-ms threshold,
REM e.g. 15) when investigating tick performance.
set WEBRTS_TICK_PROFILE=0
set WEBRTS_TICK_SLOW_MS=0
set WEBRTS_DEBUG_PATHING=0
cd /d "%~dp0"
REM `air` is the hot-reload runner; falls back to `go run` when not installed.
where air >nul 2>nul
if %errorlevel%==0 (
    air
) else (
    echo air not found on PATH; running `go run ./cmd/api` instead.
    go run ./cmd/api
)
