@echo off
set GOPATH=%USERPROFILE%\go-workspace
set PATH=%PATH%;%USERPROFILE%\go\bin;%USERPROFILE%\go-workspace\bin
cd /d "%~dp0"
air
