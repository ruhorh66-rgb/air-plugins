# Потолок opus считается в ОБРАЩЕНИЯХ, а не в ответах (VERA-PAT-000061).
#
# Повод: повтор TASK-OBS-0057 напечатал «opus: 26 из 215 ответов, ПРЕВЫШЕН ПОТОЛОК (5)»
# при ОДНОЙ раздаче opus — один субагент, ответивший 26 раз, выглядел как 26 обращений.
# Ложная тревога дороже молчания: сторож, кричащий на исправном поведении, учит не
# смотреть на его крик.
#
# Проверяется то же выражение, что стоит в run_task.ps1, на куске лога того же вида.
$ErrorActionPreference = 'Continue'
$n = 0
function ok([string]$label, [bool]$cond) {
    if (-not $cond) { throw "ПРОВАЛ: $label" }
    $script:n++
    Write-Host "ok   $label"
}

$rx = '"name"\s*:\s*"(?:Agent|Task)"\s*,\s*"input"\s*:\s*\{[^}]*?"model"\s*:\s*"([a-z0-9.\-]+)"'

function Count-Dispatches([string]$log) {
    $d = @{}
    foreach ($m in [regex]::Matches($log, $rx)) {
        $k = $m.Groups[1].Value
        $d[$k] = 1 + $(if ($d.ContainsKey($k)) { $d[$k] } else { 0 })
    }
    $d
}

# Лог того же вида, что пишет движок: одна раздача opus плюс много ОТВЕТОВ opus от
# запущенного субагента. Прежний счётчик видел здесь 4 «обращения», настоящее — одно.
$log = @'
{"type":"assistant","message":{"model":"claude-sonnet-5","content":[{"type":"tool_use","name":"Agent","input":{"model":"opus","description":"final acceptance"}}]}}
{"type":"assistant","message":{"model":"claude-opus-5","content":[{"type":"text","text":"a"}]}}
{"type":"assistant","message":{"model":"claude-opus-5","content":[{"type":"text","text":"b"}]}}
{"type":"assistant","message":{"model":"claude-opus-5","content":[{"type":"text","text":"c"}]}}
{"type":"assistant","message":{"model":"claude-sonnet-5","content":[{"type":"tool_use","name":"Agent","input":{"model":"sonnet","description":"metrics"}}]}}
'@

$d = Count-Dispatches $log
ok "обращений к opus ровно одно" ($d['opus'] -eq 1)
ok "ответы opus обращениями не считаются" (([regex]::Matches($log, '"model"\s*:\s*"claude-opus[^"]*"')).Count -eq 3)
ok "раздача sonnet посчитана отдельно" ($d['sonnet'] -eq 1)
ok "при потолке 5 тревоги нет" (-not ($d['opus'] -gt 5))

# Настоящее превышение обязано ловиться — иначе проверка была бы датчиком, всегда
# говорящим «всё хорошо» (ERR-2026-000116).
$many = (1..6 | ForEach-Object { '{"type":"assistant","message":{"model":"claude-sonnet-5","content":[{"type":"tool_use","name":"Agent","input":{"model":"opus","description":"x"}}]}}' }) -join "`n"
$d2 = Count-Dispatches $many
ok "шесть раздач opus видны как шесть" ($d2['opus'] -eq 6)
ok "и при потолке 5 это превышение" ($d2['opus'] -gt 5)

# Раздача без модели в счёт не идёт: её отбивает ENF-MODEL, исполнителя не появилось.
$noModel = '{"type":"assistant","message":{"model":"claude-sonnet-5","content":[{"type":"tool_use","name":"Agent","input":{"description":"no model"}}]}}'
$d3 = Count-Dispatches $noModel
ok "раздача без модели не считается" ($d3.Count -eq 0)

# Код отчёта обязан сравнивать с потолком именно обращения — проверяем сам файл.
$src = Get-Content (Join-Path $PSScriptRoot 'run_task.ps1') -Raw
ok "с потолком сравниваются обращения" ($src -match '\$opusCalls -gt \$maxOpus')
ok "ответы в отчёте остались, но помечены" ($src -match 'не сравниваются с потолком')

Write-Host "`n$n проверок пройдено"
