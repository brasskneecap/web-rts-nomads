@echo off
start "Frontend" /d "client\src\game-portal" cmd /k dev.bat
start "Backend" /d "server" cmd /k dev.bat
start "Tunnel" cmd /k cloudflared tunnel --url http://localhost:5173
echo Started: frontend at http://localhost:5173  ^|  backend at http://localhost:8080  ^|  cloudflared tunnel (URL printed in Tunnel window)
