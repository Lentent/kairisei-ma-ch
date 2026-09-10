@echo off
setlocal
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0Stop-Server.ps1"
set "server_exit=%ERRORLEVEL%"
echo.
pause
exit /b %server_exit%
