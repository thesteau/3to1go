@echo off
setlocal
set SCRIPT_DIR=%~dp0
set MANAGER=%SCRIPT_DIR%scripts\dev_edge.py

where py >nul 2>nul
if %ERRORLEVEL%==0 (
  py -3 "%MANAGER%" %*
  exit /b %ERRORLEVEL%
)

where python >nul 2>nul
if %ERRORLEVEL%==0 (
  python "%MANAGER%" %*
  exit /b %ERRORLEVEL%
)

echo Python 3 was not found on PATH. Install Python and try again.
exit /b 1
