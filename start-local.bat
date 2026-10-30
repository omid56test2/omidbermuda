@echo off
setlocal
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\start-local-windows.ps1"
if errorlevel 1 (
  echo.
  echo Startup failed. Read the error above.
  pause
)
endlocal
