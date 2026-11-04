[CmdletBinding()]
param([string]$CodexPath = '')
$ErrorActionPreference = 'Stop'
$node = Get-Command node.exe -ErrorAction SilentlyContinue
if (-not $CodexPath) {
    $cmd = Get-Command codex.cmd -ErrorAction SilentlyContinue
    if (-not $cmd) { $cmd = Get-Command codex.ps1 -ErrorAction SilentlyContinue }
    if ($cmd) { $CodexPath = $cmd.Source }
}
function Result([string]$status, [string[]]$ids, [string]$reason) {
    [ordered]@{schema='air-worker.codex-catalog/v1'; observed_utc=[DateTime]::UtcNow.ToString('o');
        status=$status; ids=@($ids); reason=$reason; inference='NOT_PROVEN'; payg='NOT_USED'} |
        ConvertTo-Json -Compress -Depth 5
    if ($status -ne 'LIST_RETURNED') { exit 2 }
    exit 0
}
if (-not $node -or -not $CodexPath) { Result 'NOT_PROVEN' @() 'codex_or_node_missing' }
$js = Join-Path (Split-Path -Parent $CodexPath) 'node_modules\@openai\codex\bin\codex.js'
if (-not (Test-Path -LiteralPath $js -PathType Leaf)) { Result 'NOT_PROVEN' @() 'native_app_server_missing' }
$psi = New-Object System.Diagnostics.ProcessStartInfo
$psi.FileName = $node.Source
$psi.Arguments = ('"' + $js + '" app-server --listen stdio://')
$psi.UseShellExecute = $false
$psi.RedirectStandardInput = $true
$psi.RedirectStandardOutput = $true
$psi.RedirectStandardError = $true
$psi.CreateNoWindow = $true
$p = $null
try {
    $p = [Diagnostics.Process]::Start($psi)
    $null = $p.StandardError.ReadToEndAsync()
    $p.StandardInput.WriteLine('{"id":1,"method":"initialize","params":{"clientInfo":{"name":"airworker_model_weekly","title":"AirWorker Model Catalog","version":"1.0.0"}}}')
    $p.StandardInput.Flush()
    $task = $p.StandardOutput.ReadLineAsync()
    if (-not $task.Wait(12000)) { throw 'initialize_timeout' }
    $first = $task.Result | ConvertFrom-Json
    if ($first.id -ne 1 -or $first.error) { throw 'initialize_failed' }
    $p.StandardInput.WriteLine('{"method":"initialized","params":{}}')
    $p.StandardInput.WriteLine('{"id":2,"method":"model/list","params":{}}')
    $p.StandardInput.Flush()
    $task = $p.StandardOutput.ReadLineAsync()
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    while ([DateTime]::UtcNow -lt $deadline) {
        if (-not $task.Wait(750)) { continue }
        if (-not $task.Result) { throw 'catalog_eof' }
        $reply = $task.Result | ConvertFrom-Json
        if ($reply.id -eq 2) {
            if ($reply.error) { throw 'model_list_rpc_failure' }
            $ids = @($reply.result.data | ForEach-Object { [string]$_.id })
            Result 'LIST_RETURNED' $ids ''
        }
        $task = $p.StandardOutput.ReadLineAsync()
    }
    throw 'catalog_timeout'
} catch { Result 'NOT_PROVEN' @() ([string]$_.Exception.Message) }
finally { if ($p) { if (-not $p.HasExited) { $p.Kill() }; $p.Dispose() } }
