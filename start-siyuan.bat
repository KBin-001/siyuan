@echo off
setlocal
chcp 65001 >nul
title SiYuan - Build and Start
set "SIYUAN_NODE=node"
if exist "%USERPROFILE%\.cache\codex-runtimes\codex-primary-runtime\dependencies\node\bin\node.exe" set "SIYUAN_NODE=%USERPROFILE%\.cache\codex-runtimes\codex-primary-runtime\dependencies\node\bin\node.exe"
pushd "%~dp0"
"%SIYUAN_NODE%" "%~dp0scripts\start-local-dev.cjs" %*
set "SIYUAN_LAUNCH_EXIT=%ERRORLEVEL%"
popd
if not "%SIYUAN_LAUNCH_EXIT%"=="0" pause
exit /b %SIYUAN_LAUNCH_EXIT%
