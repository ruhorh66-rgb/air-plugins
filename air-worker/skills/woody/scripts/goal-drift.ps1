#requires -Version 5.1
<#
.SYNOPSIS
    Двигатель цели: доставка вызова до бинарника. Сам расчёт живёт в `air-worker drift`.

.DESCRIPTION
    Двигатель восстановлен 13.09.2026 по прямому указанию ЛПР из Goal/Drift Loop ВЕРЫ
    (E:\-8-\air-vera\automation\controller\goal_drift.py). Правило, ради которого он
    существует, записано у ВЕРЫ в трёх местах независимо:

        рост промежуточного числа продвижением не считается,
        если расстояние до цели не уменьшается.

    ЗДЕСЬ БОЛЬШЕ НЕТ РАСЧЁТА, и это решение, а не упущение.

    14.09.2026 при правке расстояния выяснилось, что оно считается в ТРЁХ местах: функция
    measureDrift в бинарнике, её построчная копия там же в cmdDrift и этот скрипт. Все три
    писали в один журнал истории `.woody\goal-drift.jsonl`. Поправить формулу в одном месте
    значило получить двигатель, который судит один журнал двумя правилами, — и расхождение
    пришлось бы искать неделю, потому что обе половины по отдельности выглядят верными.

    Сама правка была такой. Расстояние перестало быть суммой «остаток по судье + открытые
    шаги плана»: AIR-ENV-002 нашла, что разбиение шага по предписанию THROTTLE приближало
    эскалацию, а замер ASW (24 = 1 + 23) показал, что просто выкинуть план нельзя — у
    продукта, чей судья видит план одной проверкой, закрытие шагов — единственное
    движение. Подробности — в cmd/drift.go и в SKILL.md.

    Поэтому расчёт один, в бинарнике, а скрипт — доставка, как у стража кодировки.
    Интерфейс, ключи и коды возврата сохранены полностью: петля на PowerShell (woody.ps1)
    зовёт его прежним способом и получает те же коды.

.PARAMETER ProductRoot
    Корень продукта: там лежат .goal-verdict.json, PLAN.md, run-config.json.

.PARAMETER Record
    Дописать замер в историю. Без него — чистое чтение, история не растёт.

.PARAMETER Note
    Чем был ход. Пишется в историю, чтобы потом было видно, НА ЧТО ушли ходы.

.OUTPUTS
    Код возврата: 0 — ALLOW, 1 — THROTTLE, 2 — ESCALATE (останавливать), 3 — ЖДЁТ ЛПР.
    Код 2 также означает «нечем мерить», если бинарник не найден.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$ProductRoot,
    [switch]$Record,
    [string]$Note = '',
    [switch]$Json,
    [switch]$Quiet
)

$ErrorActionPreference = 'Stop'
# Бинарник пишет UTF-8. Без этого Windows PowerShell 5.1 прочтёт его вывод кодовой
# страницей консоли, и кириллица в причине вердикта превратится в мусор ровно там, где её
# надо прочесть.
try { [Console]::OutputEncoding = [Text.UTF8Encoding]::new($false) } catch { }

# Бинарник ищется по ответу, а не по прибитому пути. Порядок важен: сперва явное
# перекрытие, затем бинарник ТОГО ЖЕ плагина, что и этот скрипт, и только потом PATH.
# В PATH может оказаться другая версия — после обновления плагина копия в PATH отстаёт,
# пока не перезапущена установка (замер 14.09.2026), — и тогда скрипт мерил бы по правилу,
# которого его собственный плагин уже не знает.
function Resolve-WorkerExe {
    if ($env:AIR_WORKER_EXE -and (Test-Path -LiteralPath $env:AIR_WORKER_EXE -PathType Leaf)) {
        return $env:AIR_WORKER_EXE
    }
    # scripts -> woody -> skills -> корень плагина: ТРИ уровня. Соседний хук однажды
    # поднимался на два, молча не находил bin и не делал ничего.
    $root = Split-Path -Parent (Split-Path -Parent (Split-Path -Parent $PSScriptRoot))
    $local = Join-Path $root 'bin\air-worker.exe'
    if (Test-Path -LiteralPath $local -PathType Leaf) { return $local }
    $cmd = Get-Command 'air-worker' -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    return $null
}

$exe = Resolve-WorkerExe
if (-not $exe) {
    # «Нечем мерить» — код 2, как у судьи, а не молчаливый ноль: отсутствие двигателя
    # неотличимо от ALLOW, если его не назвать.
    Write-Output 'НЕЧЕМ МЕРИТЬ: не найден air-worker.exe — ни AIR_WORKER_EXE, ни bin рядом с плагином, ни в PATH'
    exit 2
}

$argv = @('drift', '-product', $ProductRoot)
if ($Record) { $argv += '-record' }
if ($Note)   { $argv += @('-note', $Note) }
if ($Json)   { $argv += '-json' }
if ($Quiet)  { $argv += '-quiet' }

& $exe @argv
exit $LASTEXITCODE
