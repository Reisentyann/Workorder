@echo off
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0start.ps1"
if errorlevel 1 (
    echo.
    echo [ERROR] Start failed. See the message above.
)
echo.
pause
