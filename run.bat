@echo off
cd /d "%~dp0"

:: auto-detect Python: local .venv > PATH
set PYTHON=
if exist ".venv\Scripts\python.exe" (
    set PYTHON=.venv\Scripts\python.exe
) else (
    where python >nul 2>&1 && set PYTHON=python
)

if "%PYTHON%"=="" (
    echo [ERROR] Python not found. Please install Python 3.10+ or create .venv:
    echo   python -m venv .venv
    echo   .venv\Scripts\python.exe -m pip install -r requirements.txt
    pause
    exit /b 1
)

:: auto-install deps if missing (check PySide6)
"%PYTHON%" -c "import PySide6" 2>nul || (
    echo [协议映射] Dependencies missing, installing...
    "%PYTHON%" -m pip install -r requirements.txt
)

title 协议映射
"%PYTHON%" -m app
