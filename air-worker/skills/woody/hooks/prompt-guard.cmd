@echo off
rem Wrapper: second guard line, UserPromptSubmit. Catches a turn that was interrupted
rem before Stop ever fired. See prompt-guard.ps1 for why it does not block (exit 2) and
rem instead adds context (exit 0 + additionalContext).
rem ASCII only in this file: cmd.exe reads .cmd in the OEM codepage and would try to
rem execute mangled Cyrillic comments as commands. Measured 11.09.2026.
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0prompt-guard.ps1"
exit /b %ERRORLEVEL%
