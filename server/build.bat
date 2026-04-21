@echo off
cd /d "%~dp0"
"%USERPROFILE%\go\bin\go.exe" build -o ./tmp/api.exe ./cmd/api
