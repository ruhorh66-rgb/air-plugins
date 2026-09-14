@echo off
rem Wrapper: raise the product tray icon when the plugin loads.
rem ASCII only in this file: cmd.exe reads .cmd in the OEM codepage and would try to
rem execute mangled Cyrillic comments as commands. Measured 11.09.2026.
rem
rem The binary is called DIRECTLY, without PowerShell in between: this hook runs on
rem every session start, and a PowerShell startup costs about ten times the whole job.
rem The command is idempotent - see cmd/trayctl_windows.go for why several sessions
rem raising it at once still end up with exactly one icon.
rem
rem Errors are swallowed on purpose. A hook that breaks session start because an icon
rem did not come up would be removed on the first bad day, and with it the icon.
setlocal
set "AWEXE=%~dp0..\..\..\bin\air-worker.exe"
if not exist "%AWEXE%" goto :eof
"%AWEXE%" tray -ensure -quiet
:eof
exit /b 0
