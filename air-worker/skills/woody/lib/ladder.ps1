<#
.SYNOPSIS
  Разрешение имени ступени в позицию на лестнице продукта. Общая библиотека.

.DESCRIPTION
  Задача 6, 12.09.2026. Ступень шага искалась в woody.ps1 (строка 486) ТОЧНЫМ совпадением:
  [array]::IndexOf($ladder, $step.Tier). План говорит «sonnet», а лестница держит только
  «sonnet:medium» и «sonnet:max» — IndexOf возвращает -1, и код МОЛЧА подставлял
  $tierIndex = 0, то есть script. Объявленная ступень тихо подменялась самой низкой, и
  петля карабкалась с нуля — тот самый класс тихой подстановки умолчания, который в
  остальном коде старательно вычищен.

  Функция здесь ОДНА и общая для woody.ps1 (выбор ступени шага) и turn-guard.ps1/
  prompt-guard.ps1 (проверка, что ступень, названная в строке субагента, существует в
  лестнице продукта) — переиспользуется, а не копируется дважды: две копии разойдутся.

  Правило разрешения, по порядку:
    1. точное совпадение строки ступени с элементом лестницы;
    2. при промахе — совпадение по имени модели ДО двоеточия (лестница держит
       'sonnet:medium', шаг называет просто 'sonnet' — это тот же вендор с тем же
       вопросом «сколько платить», без выбора усилия);
    3. при промахе обоих — Found=$false. Молчаливого нуля здесь больше нет: решение,
       что делать при непопадании, принимает вызывающий (отказ с названной причиной
       в woody.ps1, запись в список недостающего в turn-guard.ps1).
#>

function Resolve-LadderTier {
    param(
        [string[]]$Ladder,
        [string]$Tier
    )

    if (-not $Tier) {
        return [pscustomobject]@{ Found = $false; Index = -1; MatchKind = 'none' }
    }
    if (-not $Ladder -or $Ladder.Count -eq 0) {
        return [pscustomobject]@{ Found = $false; Index = -1; MatchKind = 'no-ladder' }
    }

    $idx = [array]::IndexOf($Ladder, $Tier)
    if ($idx -ge 0) {
        return [pscustomobject]@{ Found = $true; Index = $idx; MatchKind = 'exact' }
    }

    $name = $Tier
    $sep = $Tier.IndexOf(':')
    if ($sep -gt 0) { $name = $Tier.Substring(0, $sep) }

    for ($i = 0; $i -lt $Ladder.Count; $i++) {
        $rung = [string]$Ladder[$i]
        $rungName = $rung
        $rsep = $rung.IndexOf(':')
        if ($rsep -gt 0) { $rungName = $rung.Substring(0, $rsep) }
        if ($rungName -eq $name) {
            return [pscustomobject]@{ Found = $true; Index = $i; MatchKind = 'model-prefix' }
        }
    }

    return [pscustomobject]@{ Found = $false; Index = -1; MatchKind = 'none' }
}
