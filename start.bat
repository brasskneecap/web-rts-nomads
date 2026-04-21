@echo off
start "Frontend" /d "client\src\game-portal" cmd /k dev.bat
start "Backend" /d "server" cmd /k dev.bat
echo Started: frontend at http://localhost:5173  ^|  backend at http://localhost:8080
