@echo off
setlocal
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0Start-Server.ps1"
set "server_exit=%ERRORLEVEL%"
echo.
if not "%server_exit%"=="0" echo Server startup failed. See the error above.
if "%server_exit%"=="0" echo Server is ready. Admin: http://127.0.0.1:26022/
pause
exit /b %server_exit%
