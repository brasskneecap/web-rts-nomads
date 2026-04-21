@echo off
start "Frontend" cmd /k "cd /d "C:\Users\Chad Anderson\web-rts-nomads\client\src\game-portal" && npm run dev"
start "Backend" cmd /k "set PATH=%PATH%;C:\Users\Chad Anderson\go\bin && set GOPATH=C:\Users\Chad Anderson\go-workspace && cd /d "C:\Users\Chad Anderson\web-rts-nomads\server" && "C:\Users\Chad Anderson\go-workspace\bin\air.exe" -c .air.toml"
echo Started: frontend at http://localhost:5173  ^|  backend at http://localhost:8080
