<#
.SYNOPSIS
  След решений: кто из ролей что решил на этом ходу.

.DESCRIPTION
  Заведено 12.09.2026 по прямому запросу ЛПР: «чтобы я видел, что происходит, и
  понимал, куда вносить правки».

  Причина глубже удобства. РЕЗУЛЬТАТ СТРАЖА — ЭТО ОТСУТСТВИЕ СОБЫТИЯ, и увидеть
  его нечем: исправно работающий сторож молчит ровно так же, как не запущенный.
  Сегодня это стоило восьми часов — режим числился включённым, хуки были мертвы,
  и отличить одно от другого можно было только замером зонда. След делает работу
  механизма наблюдаемой, не спрашивая человека смотреть в четыре разных файла.

  Пишется по строке на решение, JSONL, отдельным файлом на сессию. Дозапись
  построчная: на машине работают несколько сессий, и перечитывание файла целиком
  дало бы гонку.

  ЛЮБАЯ ОШИБКА ЗДЕСЬ ГЛОТАЕТСЯ. След — диагностика, а не работа; сторож, который
  ломает ход из-за собственного журнала, хуже отсутствующего следа.
#>

function Write-WoodyTrace {
    param(
        [Parameter(Mandatory)][string]$SessionId,
        [Parameter(Mandatory)][string]$Role,      # страж | второй рубеж | судья | учёт | режим | петля
        [Parameter(Mandatory)][string]$Event,     # Stop | UserPromptSubmit | SubagentStart | -On | ...
        [Parameter(Mandatory)][string]$Decision,  # пропустил | отклонил | записал | пропущено-нечем
        [string]$Detail = '',
        [hashtable]$Extra
    )
    try {
        if (-not $SessionId) { return }
        $dir = Join-Path $env:ProgramData 'AIR OS\State'
        if (-not (Test-Path -LiteralPath $dir)) { New-Item -ItemType Directory -Force -Path $dir | Out-Null }
        $rec = [ordered]@{
            ts       = (Get-Date).ToString('s')
            role     = $Role
            event    = $Event
            decision = $Decision
            detail   = $Detail
        }
        if ($Extra) { foreach ($k in $Extra.Keys) { $rec[$k] = $Extra[$k] } }
        $line = ($rec | ConvertTo-Json -Depth 4 -Compress)
        $path = Join-Path $dir ("woody-trace-$SessionId.jsonl")
        # Дозапись с повтором: два хука одной сессии могут писать одновременно.
        for ($attempt = 1; $attempt -le 3; $attempt++) {
            try {
                [System.IO.File]::AppendAllText($path, $line + "`r`n", (New-Object System.Text.UTF8Encoding($false)))
                return
            } catch {
                if ($attempt -eq 3) { return }
                Start-Sleep -Milliseconds (20 * $attempt)
            }
        }
    } catch { return }
}

function Get-WoodyTrace {
    param(
        [Parameter(Mandatory)][string]$SessionId,
        [int]$Last = 20
    )
    $path = Join-Path $env:ProgramData ("AIR OS\State\woody-trace-$SessionId.jsonl")
    if (-not (Test-Path -LiteralPath $path)) { return @() }
    $out = @()
    foreach ($line in [System.IO.File]::ReadAllLines($path, [System.Text.Encoding]::UTF8)) {
        if (-not $line.Trim()) { continue }
        try { $out += ($line | ConvertFrom-Json) } catch { }
    }
    if ($out.Count -le $Last) { return $out }
    return $out[($out.Count - $Last)..($out.Count - 1)]
}
