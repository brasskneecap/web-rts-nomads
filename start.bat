@echo off
start "Frontend" /d "%USERPROFILE%\web-rts-nomads\client\src\game-portal" cmd /k dev.bat
start "Backend" /d "%USERPROFILE%\web-rts-nomads\server" cmd /k dev.bat
echo Started: frontend at http://localhost:5173  ^|  backend at http://localhost:8080
