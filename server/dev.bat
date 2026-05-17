@echo off
set GOPATH=%USERPROFILE%\go-workspace
set PATH=%PATH%;%USERPROFILE%\go\bin;%USERPROFILE%\go-workspace\bin
REM Tick-profile: prints per-section timings to stderr every ~5s. Remove or
REM set to "0" to disable once we're done debugging slow ticks.
set WEBRTS_TICK_PROFILE=1
cd /d "%~dp0"
air
