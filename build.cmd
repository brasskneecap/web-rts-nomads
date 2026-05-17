@echo off
:: Thin wrapper so `build.cmd package` works from cmd.exe without typing the
:: full PowerShell incantation. All args are forwarded as-is to build.ps1.
::
:: Examples:
::   build              ::  shows help
::   build package      ::  full pipeline (spa -> sidecar -> shell)
::   build test         ::  go test + cargo test
::   build clean        ::  remove built artefacts
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0build.ps1" %*
exit /b %errorlevel%
