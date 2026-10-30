@echo off
setlocal
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\setup-windows.ps1"
if errorlevel 1 (
  echo.
  echo Setup failed. Read the error above.
  pause
)
endlocal
