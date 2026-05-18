@echo off
rem Thin wrapper so `build.cmd package` works from cmd.exe without typing the
rem full PowerShell incantation. All args are forwarded as-is to build.ps1.
rem
rem Examples:
rem   build                     - show help
rem   build package             - full pipeline (no Steam)
rem   build package -Steam      - Steam build (raw .exe, no MSI)
rem   build shell -Steam        - same as above, skip SPA + sidecar rebuild
rem   build test                - run go test + cargo test
rem   build clean               - remove built artefacts
rem
rem The -Steam flag produces a raw .exe at
rem desktop\src-tauri\target\release\nomads-desktop.exe instead of an MSI,
rem because cargo tauri build does not propagate --features steam reliably.
rem MSI bundling for Steam builds is a Phase 3 follow-up.
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0build.ps1" %*
exit /b %errorlevel%
