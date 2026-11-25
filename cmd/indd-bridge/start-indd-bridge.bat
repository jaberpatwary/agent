@echo off
rem Starts The Daily Star InDesign bridge (keeps running in this window).
rem Put a shortcut to this file in the Startup folder (Win+R, shell:startup)
rem to start it at sign-in.
cd /d "%~dp0"
if not exist indd-bridge.exe (
  echo indd-bridge.exe is not in this folder. Ask IT, or build it:
  echo   go build -o indd-bridge.exe ./cmd/indd-bridge
  pause
  exit /b 1
)
indd-bridge.exe
pause
