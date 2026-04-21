@echo off
echo Stopping servers...
for /f "tokens=5" %%a in ('netstat -ano ^| findstr ":8080 :5173"') do taskkill /PID %%a /F >nul 2>&1
echo Done.
