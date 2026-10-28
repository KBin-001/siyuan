@echo off
setlocal
title SiYuan v2 - Build and Run
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\build-and-run-windows.ps1" %*
if errorlevel 1 (
    echo.
    echo Build failed. See the message above and .cache\windows-desktop\build.log.
    pause
    exit /b 1
)
