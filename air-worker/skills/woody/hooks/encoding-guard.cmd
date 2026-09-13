@echo off
rem Wrapper: the hook "command" field takes an executable path, and a .ps1 is not one.
rem stdin is inherited, so the event JSON reaches the script.
rem Windows PowerShell 5.1 on purpose: present on every machine of the contour.
rem ASCII only in this file: cmd.exe reads .cmd in the OEM codepage.
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0encoding-guard.ps1"
exit /b 0
