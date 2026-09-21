# Polls adapter status back to back, like a caller (Hermes, tray) would, until stop.flag appears.
param([string]$Exe, [string]$Dir, [int]$MaxSeconds = 60)
$sw = [Diagnostics.Stopwatch]::StartNew()
$n = 0
while ($sw.Elapsed.TotalSeconds -lt $MaxSeconds -and -not (Test-Path (Join-Path $Dir 'stop.flag'))) {
    & $Exe adapter -action status -product $Dir *> $null
    $n++
}
'status polls: ' + $n
