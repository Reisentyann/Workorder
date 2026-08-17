@echo off
setlocal
cd /d "%~dp0backend"

echo ==========================================
echo   AI Ticket Workbench - Start (prebuilt)
echo ==========================================
echo.

if not exist workbench.exe (
    echo [ERROR] workbench.exe not found. Run start.bat to build first.
    pause
    exit /b 1
)

echo Starting service (demo mode)...
start "workbench-service" cmd /c "workbench.exe -demo -frontend ..\frontend"

timeout /t 2 /nobreak >nul
start "" http://localhost:8080

echo.
echo Service running at http://localhost:8080
echo Close the "workbench-service" window to stop the service.
echo.
pause
