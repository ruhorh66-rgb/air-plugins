@echo off
rem Wrapper for subagent bookkeeping. One wrapper for both events: SubagentStart and
rem SubagentStop are told apart inside the script by hook_event_name from the JSON.
rem ASCII only: see turn-guard.cmd.
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0subagent-track.ps1"
exit /b %ERRORLEVEL%
