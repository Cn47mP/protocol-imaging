@echo off
cd /d "%~dp0"

set PYTHON=
if exist ".venv\Scripts\python.exe" (
    set PYTHON=.venv\Scripts\python.exe
) else (
    where python >nul 2>&1 && set PYTHON=python
)

if "%PYTHON%"=="" (
    echo [ERROR] Python not found.
    pause
    exit /b 1
)

title –≠“È”≥…‰ [DEBUG]
"%PYTHON%" -m app --debug
pause
