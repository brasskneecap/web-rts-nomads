@echo off
set GOPATH=%USERPROFILE%\go-workspace
set PATH=%PATH%;%USERPROFILE%\go\bin;%USERPROFILE%\go-workspace\bin
REM Tick-profile: prints per-section timings to stderr every ~5s and fires an
REM immediate [tick-profile][SLOW] line on any single section >= WEBRTS_TICK_SLOW_MS.
REM Set WEBRTS_TICK_PROFILE=0 (and remove WEBRTS_TICK_SLOW_MS) to disable.
set WEBRTS_TICK_PROFILE=1
set WEBRTS_TICK_SLOW_MS=5
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
