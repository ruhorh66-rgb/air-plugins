@echo off
rem Wrapper that denies leaving orchestration mode without LPR approval. PreToolUse.
rem ASCII only: see turn-guard.cmd.
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0mode-guard.ps1"
exit /b %ERRORLEVEL%
