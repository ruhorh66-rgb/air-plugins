<#
.SYNOPSIS
  Проверка судьи: лестница исполнима на всех ступенях, а не только на нулевой.

.DESCRIPTION
  Основание — две молчаливые поломки, найденные 12.09.2026 разбором:

    ступени 2-3  роняли ValueError при отсутствии команды вызова вместо пропуска
                 с названной причиной; починено;
    ступени 4-5  звали claude.exe по прибитому пути npm-установки, которой на машине
                 НЕ СУЩЕСТВУЕТ — верх лестницы был оборван; починено.

  Обе давали метрику «100 % закрытых на нулевом уровне», и она читалась как дешевизна,
  а означала обрыв: выше нуля дороги нет, и подниматься некуда. Эта проверка нужна
  затем, чтобы такой обрыв больше не выглядел успехом.

  Коды: 0 — все ступени достижимы; 1 — есть оборванная; 2 — проверять нечем.
#>
[CmdletBinding()]
param()

$ErrorActionPreference = 'Continue'

$productRoot = Split-Path -Parent $PSScriptRoot
$scriptsDir = Join-Path $productRoot 'skills\run-worker-task\scripts'
$ladder = Join-Path $scriptsDir 'ladder.py'
$judgeRun = Join-Path $scriptsDir 'claude_judge_run.py'

foreach ($f in @($ladder, $judgeRun)) {
    if (-not (Test-Path -LiteralPath $f -PathType Leaf)) {
        Write-Output "НЕЧЕМ ПРОВЕРИТЬ: нет $f"
        exit 2
    }
}

$passed = 0
$failed = 0
function Assert-That([string]$title, [scriptblock]$check) {
    try {
        if (& $check) { Write-Output "  [PASS]  $title"; $script:passed++ }
        else { Write-Output "  [FAIL]  $title"; $script:failed++ }
    } catch {
        Write-Output "  [FAIL]  $title -- $($_.Exception.Message)"
        $script:failed++
    }
}

$ladderText = [System.IO.File]::ReadAllText($ladder, [System.Text.Encoding]::UTF8)
$judgeText  = [System.IO.File]::ReadAllText($judgeRun, [System.Text.Encoding]::UTF8)

# 1. Ступени 2-3: отсутствие команды даёт ПРОПУСК, а не исключение.
Assert-That 'ступень 2 при отсутствии команды пропускает, а не бросает исключение' {
    $ladderText -match 'openrouter_cmd' -and $ladderText -notmatch 'raise ValueError\([^)]*openrouter_cmd'
}
Assert-That 'ступень 3 при отсутствии команды пропускает, а не бросает исключение' {
    $ladderText -match 'codex_cmd' -and $ladderText -notmatch 'raise ValueError\([^)]*codex_cmd'
}

# 2. Подъём без названной причины остаётся ЗАПРЕЩЁННЫМ. Это не должно «починиться»
#    заодно: молчаливая эскалация дала рою 88 % Opus.
Assert-That 'подъём без названной причины по-прежнему запрещён исключением' {
    $ladderText -match 'escalate\(\) требует непустой reason'
}

# 3. Ступени 4-5: инструмент ищется по ответу, а не прибитым путём.
Assert-That 'путь к claude не прибит в коде' {
    $judgeText -notmatch 'AppData\\\\Roaming\\\\npm\\\\node_modules'
}
Assert-That 'claude резолвится на этой машине' {
    $py = $null
    foreach ($n in @('python','python3')) { $c = Get-Command $n -ErrorAction SilentlyContinue; if ($c) { $py = $c.Source; break } }
    if (-not $py) { throw 'интерпретатор не найден — это нечем проверить' }
    $out = & $py -c "import sys; sys.path.insert(0, r'$scriptsDir'); import claude_judge_run as m; print(m._resolve_claude_exe())" 2>&1 | Out-String
    $resolved = ($out -split "`r?`n" | Where-Object { $_.Trim() } | Select-Object -First 1)
    $resolved -and (Test-Path -LiteralPath $resolved.Trim())
}

# 4. Задание уходит исполнителю не аргументом. Требование 3 нормы AUTO-080,
#    купленное там дважды и здесь в третий раз.
Assert-That 'промпт передаётся через стандартный ввод, а не в командной строке' {
    $judgeText -match 'input=prompt'
}

Write-Output ''
Write-Output ("прошло {0}, провалено {1}" -f $passed, $failed)
if ($failed -gt 0) { exit 1 }
exit 0
