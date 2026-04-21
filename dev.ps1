Start-Process cmd -ArgumentList "/k", "cd /d `"C:\Users\Chad Anderson\web-rts-nomads\client\src\game-portal`" && npm run dev"
Start-Process cmd -ArgumentList "/k", "set GOPATH=C:\Users\Chad Anderson\go-workspace && set PATH=%PATH%;C:\Users\Chad Anderson\go\bin;C:\Users\Chad Anderson\go-workspace\bin && cd /d `"C:\Users\Chad Anderson\web-rts-nomads\server`" && air"
Write-Host "Started: frontend at http://localhost:5173  |  backend at http://localhost:8080"
Write-Host "Both servers will auto-reload on file changes."
