<#
.SYNOPSIS
  Единая проверка формата хода. Общая для turn-guard.ps1 (Stop) и prompt-guard.ps1
  (UserPromptSubmit) — переиспользуется, а не копируется: две копии разойдутся.

.DESCRIPTION
  Перенесено из turn-guard.ps1 при добавлении второго рубежа (задача 1, 12.09.2026).
  Логика семи проверок не менялась при переносе — только вынесена в функцию, чтобы
  её звал ещё один хук. Восьмая проверка (ступень в строке субагента) добавлена здесь
  же, задача 2, 12.09.2026: «лестница и режим не связаны» — проверка формата не смотрит
  на orchestration.enabled, она смотрит на то, что реально написано в строках субагентов.
#>

. (Join-Path $PSScriptRoot 'ladder.ps1')

function Test-TurnFormat {
    param(
        [string]$Msg,
        [array]$Running,             # живые субагенты (из woody-agents-*.json)
        [int]$DeclaredSubagents,     # mode.subagents этой сессии
        [string[]]$Ladder            # ladder продукта из run-config.json; $null/@() — неизвестна
    )

    $missing = @()
    if (-not $Msg) { return @('сообщение пустое') }

    # 1. Строка состояния.
    if ($Msg -notmatch '(?m)^\s*Фаза\s') {
        $missing += 'строка состояния «Фаза <шаг> · <заголовок> · ступень <tier> · <КТО ИСПОЛНЯЕТ> · <статус>»'
    }

    # 2. Строка на каждого живого субагента. Форма ищется ТОЧНАЯ — «субагент N/M»
    # (см. историю в turn-guard.ps1: подстрока «субагентов 2» в строке ведущей иначе
    # считается за строку субагента и маскирует пропуск).
    $subagentLines = @([regex]::Matches($Msg, '(?m)^\s*Фаза[^\r\n]*субагент\s*\d+\s*/\s*\d+[^\r\n]*$'))
    if ($Running -and $Running.Count -gt 0) {
        if ($subagentLines.Count -lt $Running.Count) {
            $names = ($Running | ForEach-Object { $_.type }) -join ', '
            $missing += "строка на каждого живого субагента (их $($Running.Count): $names), найдено $($subagentLines.Count)"
        }
    }

    # 2b. Задача 2: КАЖДАЯ строка субагента обязана называть «ступень <tier>», и
    # названная ступень обязана существовать в лестнице продукта, когда лестница
    # известна (run-config.json найден). Формат строки состояния сверх этого не
    # меняется — согласован ЛПР, Ядро п. 1.2.
    foreach ($m in $subagentLines) {
        $tm = [regex]::Match($m.Value, '·\s*ступень\s+(?<tier>[^\s·]+)')
        if (-not $tm.Success) {
            $missing += "строка субагента без названной ступени: «$($m.Value.Trim())»"
            continue
        }
        if ($Ladder -and $Ladder.Count -gt 0) {
            $r = Resolve-LadderTier -Ladder $Ladder -Tier $tm.Groups['tier'].Value
            if (-not $r.Found) {
                $missing += "строка субагента называет ступень '$($tm.Groups['tier'].Value)', " +
                            "которой нет в лестнице продукта ($($Ladder -join ', ')): «$($m.Value.Trim())»"
            }
        }
    }

    # Ноль живых при ОБЪЯВЛЕННЫХ субагентах обязан быть объяснён.
    if ((-not $Running -or $Running.Count -eq 0) -and $DeclaredSubagents -gt 0) {
        $zeroPattern = '(?m)^\s*Фаза.*субагентов\s*0\s*из\s*\d+.*·.*\S'
        if ($Msg -notmatch $zeroPattern) {
            $missing += "строка «Фаза … · субагентов 0 из $DeclaredSubagents · <почему их нет>» — " +
                        "режим объявляет $DeclaredSubagents, живых нет, причина не названа"
        }
    }

    # 3, 4. Две обязательные последние строки.
    if ($Msg -notmatch '(?m)^\s*Дальше\s*:')      { $missing += 'строка «Дальше:»' }
    if ($Msg -notmatch '(?m)^\s*От тебя жду\s*:') { $missing += 'строка «От тебя жду:»' }

    # 5. Замер.
    if ($Msg -notmatch '(?m)^\s*Замер\s*:') {
        $missing += 'строка «Замер:» — цена хода называется, а не подразумевается'
    }

    # 6. Постановка.
    if ($Msg -notmatch '(?m)^\s*Постановка\s*:') {
        $missing += 'строка «Постановка:» — либо ссылка на пакет и шаг плана, ' +
                    'либо прямо: «жёстко не прописана — выполняю сам, по своему пониманию цели»'
    }

    # 7. ПРОСТОЙ: «От тебя жду: не жду ничего» и названный следующий шаг вместе
    # означают, что ход заканчивать нельзя.
    $awaitNothing = $Msg -match '(?m)^\s*От тебя жду\s*:\s*(не\s+жду\s+)?ничего\b'
    $nextNothing  = $Msg -match '(?m)^\s*Дальше\s*:\s*ничего\b'
    if ($awaitNothing -and -not $nextNothing) {
        $missing += 'ПРОСТОЙ: ты назвал следующий шаг и одновременно написал, что ничего ' +
                    'не ждёшь. Тогда ход не заканчивают — его делают. Либо работай дальше, ' +
                    'либо напиши «Дальше: ничего» и назови, почему работа исчерпана, либо ' +
                    'назови в «От тебя жду» то, чего ждёшь на самом деле'
    }

    return $missing
}
