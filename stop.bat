@echo off
echo Stopping servers...
powershell -NoProfile -Command "Get-CimInstance Win32_Process | Where-Object { $_.Name -eq 'cmd.exe' -and ($_.CommandLine -like '*dev.bat*' -or $_.CommandLine -like '*cloudflared*') } | ForEach-Object { & taskkill /PID $_.ProcessId /T /F }" >nul 2>&1
taskkill /IM cloudflared.exe /F >nul 2>&1
for /f "tokens=5" %%a in ('netstat -ano ^| findstr ":8080 :5173"') do taskkill /PID %%a /F >nul 2>&1
echo Done.
