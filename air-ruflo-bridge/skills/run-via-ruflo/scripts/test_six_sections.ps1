# Проверка гейта формы постановки: шесть разделов (AIR_VIBECODING § 2а).
#
# Проверяются те самые выражения, что стоят в run_task.ps1, на настоящих файлах задач
# контура — а не на выдуманных строках. Живая постановка обязана проходить, урезанная
# обязана отвергаться с ИМЕНЕМ недостающего раздела: отказ без имени человек читает как
# «сломалось», а не как «допиши раздел».
$ErrorActionPreference = 'Stop'
$n = 0
function ok([string]$label, [bool]$cond) {
    if (-not $cond) { throw "ПРОВАЛ: $label" }
    $script:n++
    Write-Host "ok   $label"
}

$required = [ordered]@{
    '1. Цель как состояние'  = '(?m)^##\s*1[\.\)]\s*Цель'
    '2. Механизм реализации' = '(?m)^##\s*2[\.\)]\s*Механизм'
    '3. Источники'           = '(?m)^##\s*3[\.\)]\s*Источники'
    '4. Результат'           = '(?m)^##\s*4[\.\)]\s*Результат'
    '5. Метрики'             = '(?m)^##\s*5[\.\)]\s*Метрики'
    '6. Настройки прогона'   = '(?m)^##\s*6[\.\)]\s*Настройки'
}
function Missing([string]$text) {
    $out = @()
    foreach ($k in $required.Keys) { if ($text -notmatch $required[$k]) { $out += $k } }
    ,$out
}

$wiki = 'E:\-5-\010_Task_Control_Platform\02_WIKI'

# 1. Живые постановки, написанные ПОСЛЕ введения стандарта, проходят целиком.
foreach ($id in '0056', '0057') {
    $f = Get-ChildItem $wiki -Filter "TASK-OBS-$id*.md" | Select-Object -First 1
    ok "$id — живая постановка проходит" ((Missing (Get-Content $f.FullName -Raw)).Count -eq 0)
}

# 2. Постановка старого образца обязана отвергаться — иначе гейт бесполезен.
#    0051 заведена 11.08, до стандарта: «Зачем / Что требуется / Границы / Как поймём».
$old = Get-ChildItem $wiki -Filter 'TASK-OBS-0051*.md' | Select-Object -First 1
$missOld = Missing (Get-Content $old.FullName -Raw)
ok "0051 — постановка старого образца отвергается" ($missOld.Count -gt 0)
ok "и недостающие разделы названы поимённо" ($missOld -join '; ').Length -gt 0

# 3. Пропажа ровно одного раздела ловится, и называется именно он.
$text = (Get-Content (Get-ChildItem $wiki -Filter 'TASK-OBS-0057*.md' | Select-Object -First 1).FullName -Raw)
$cut = $text -replace '(?m)^##\s*5\.\s*Метрики', '## 5. Замеры'
$missOne = Missing $cut
ok "пропажа одного раздела поймана" ($missOne.Count -eq 1)
ok "назван именно он" ($missOne[0] -eq '5. Метрики')

# 4. Ручной прогон без TaskId гейт не трогает — проверяется в run_task.ps1 условием,
#    здесь фиксируем само условие текстом, чтобы его нельзя было потерять молча.
$src = Get-Content (Join-Path $PSScriptRoot 'run_task.ps1') -Raw
ok "гейт применяется только к задачам очереди" ($src -match 'if \(\$TaskId\) \{')
ok "отказ идёт кодом 11" ($src -match 'exit 11')

Write-Host "`n$n проверок пройдено"
