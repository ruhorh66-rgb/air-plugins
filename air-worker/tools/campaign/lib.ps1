# Shared helpers for the 0.10.11-fix campaign scripts. ASCII only: judge checks run under Windows PowerShell 5.1.
$script:Root = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$script:ResDir = Join-Path $script:Root '.campaign\results'
$script:CandExe = Join-Path $script:Root '.campaign\candidate\air-worker.exe'

# Hash of all Go sources under cmd/: a result file is valid only for the sources it was produced from.
function Get-SrcHash {
    $cmdDir = Join-Path $script:Root 'cmd'
    $sb = New-Object System.Text.StringBuilder
    # Ordinal order: Sort-Object is culture-sensitive and differs between Windows PowerShell 5.1 and pwsh 7.
    $names = @(Get-ChildItem -LiteralPath $cmdDir -Recurse -File -Filter *.go | ForEach-Object { $_.FullName })
    [Array]::Sort($names, [System.StringComparer]::Ordinal)
    foreach ($n in $names) {
        $h = (Get-FileHash -Algorithm SHA256 -LiteralPath $n).Hash
        [void]$sb.Append($n.Substring($cmdDir.Length)).Append(':').Append($h).Append(';')
    }
    $sha = [System.Security.Cryptography.SHA256]::Create()
    $bytes = [System.Text.Encoding]::UTF8.GetBytes($sb.ToString())
    ([BitConverter]::ToString($sha.ComputeHash($bytes))).Replace('-', '')
}

function Write-Result([string]$Name, [bool]$Ok, [string]$Detail) {
    New-Item -ItemType Directory -Force -Path $script:ResDir | Out-Null
    $o = [ordered]@{ check = $Name; ok = $Ok; src = (Get-SrcHash); at = (Get-Date).ToString('s'); detail = $Detail }
    ($o | ConvertTo-Json -Depth 5) | Set-Content -Encoding UTF8 -LiteralPath (Join-Path $script:ResDir ($Name + '.json'))
}
