@echo off
echo 正在打包 protocol-imaging ...
python -m PyInstaller protocol-imaging.spec --noconfirm
if %errorlevel% neq 0 (
    echo.
    echo 打包失败，请检查上方错误信息。
    pause
    exit /b 1
)
echo.
echo 打包完成：dist\protocol-imaging.exe
pause
