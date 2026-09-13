<#
.SYNOPSIS
  Запрет на самовольный выход из режима оркестрации.

.DESCRIPTION
  Вторая половина лечения отказа 11.09.2026. Страж хода не даёт закончить сообщение без
  формата, но пока режим можно выключить одной командой, страж обходится тривиально:
  выключил — и пиши как хочешь.

  Поэтому вешается на PreToolUse и ОТКЛОНЯЕТ любой вызов, выключающий режим, если рядом
  нет отметки согласования ЛПР. Формулировка ЛПР дословно по смыслу: «ты не можешь выйти
  без моего согласования».

  Отметка — файл woody-mode-off-approved.json в каталоге состояния. Его кладёт ЛПР, а не
  сессия: сессия, которая сама себе выписывает разрешение, — это не согласование.

  Пропускает при любой своей ошибке (правило стража: сломанный охранник не держит работу).
#>
[CmdletBinding()]
param()

$ErrorActionPreference = 'Continue'

try {
    # Stdin явно как UTF-8: [Console]::In в PowerShell 5.1 декодирует кодовой страницей
    # консоли, и кириллица в команде приходит мусором. Здесь это тише, чем в turn-guard
    # (образцы поиска латинские), но команда с кириллицей всё равно исказилась бы.
    $stdinStream = [Console]::OpenStandardInput()
    $stdinReader = New-Object System.IO.StreamReader($stdinStream, (New-Object System.Text.UTF8Encoding($false)))
    $raw = $stdinReader.ReadToEnd()
    if (-not $raw) { exit 0 }
    $ev = $raw | ConvertFrom-Json

    $cmd = ''
    if ($ev.tool_input) {
        if ($ev.tool_input.command) { $cmd = [string]$ev.tool_input.command }
        elseif ($ev.tool_input.file_path) { $cmd = [string]$ev.tool_input.file_path }
    }
    if (-not $cmd) { exit 0 }

    # СОСТОЯНИЕ ПОСЕССИОННОЕ (12.09.2026, решение ЛПР «Вуди должен быть
    # многопользовательским»). Ключ — session_id события; имя файла и есть привязка.
    $stateDir = Join-Path $env:ProgramData 'AIR OS\State'
    $sessionKey = [string]$ev.session_id
    if (-not $sessionKey) { exit 0 }
    $approval = Join-Path $stateDir "woody-mode-off-approved-$sessionKey.json"

    # ПЕРЕДЕЛКА 11.09.2026 по прямому указанию ЛПР: «выход из режима должен быть по
    # моему гейту, не по файлу». Дыра была ровно в этом — страж матчил только правку
    # woody-mode.json и вызов mode.ps1 -Off, а СОЗДАНИЕ самого файла-согласования не
    # проверял вообще. Сессия могла заранее положить себе approval с любым session_id,
    # включить режим под этот же id и выйти без единого живого решения ЛПР — файл
    # существовал бы, но согласования не было. Проверка идёт ПЕРВОЙ, до всех проверок
    # состояния режима и БЕЗ исключений: этой сессии нельзя коснуться approval-файла ни
    # при включённом, ни при выключенном режиме, ни разу. Кладёт его ЛПР из своего
    # терминала — не отсюда.
    # Проверка требует ПОЛНОГО путевого контекста рядом с именем файла (ProgramData /
    # AIR OS...State), а не голого имени — иначе любой текст, ОБСУЖДАЮЩИЙ этот же файл
    # (документация, это самое сообщение коммита) ложно матчится как попытка записи.
    # Пойман 12.09.2026 на живом прогоне: коммит, описывающий сам фикс словами «...
    # woody-mode-off-approved.json...» и «...Set-Content...» в разных предложениях
    # одного абзаца, был отклонён этим же правилом раньше, чем успел исполниться.
    # Имя согласования стало посессионным (woody-mode-off-approved-<session_id>.json),
    # поэтому образец ловит ЛЮБОЙ суффикс: запрет касается согласования любой сессии, а
    # не только своей. Сессия, кладущая разрешение соседке, — это тот же обход гейта.
    $approvedName = 'woody-mode-off-approved[^\s''"|>]*\.json'
    $pathNearName = $cmd -match ('(?i)ProgramData[^\r\n]{0,80}' + $approvedName) -or
                    $cmd -match ('(?i)AIR\s?OS[\\/ ]?State[^\r\n]{0,40}' + $approvedName) -or
                    $cmd -match ('(?i)' + $approvedName + '[^\r\n]{0,40}AIR\s?OS[\\/ ]?State')
    if ($pathNearName -and
        $cmd -match 'Set-Content|Out-File|New-Item|Add-Content|Remove-Item|Copy-Item|del\s|rm\s|cp\s|copy\s|>') {
        $why0 = "Файл согласования выхода woody-mode-off-approved.json не может появиться, " +
                "измениться или исчезнуть из этой сессии — ни при каком состоянии режима. Это и " +
                "есть гейт ЛПР: его кладёт ЛПР со своего терминала, а не сессия сама себе. " +
                "Прогони это действие не через Claude Code."
        $json0 = @{ hookSpecificOutput = @{ hookEventName = 'PreToolUse'; permissionDecision = 'deny'; permissionDecisionReason = $why0 } } |
            ConvertTo-Json -Depth 4 -Compress
        $out0 = [Console]::OpenStandardOutput()
        $bytes0 = [System.Text.Encoding]::UTF8.GetBytes($json0)
        $out0.Write($bytes0, 0, $bytes0.Length)
        $out0.Flush()
        exit 2
    }

    $modeFile = Join-Path $stateDir "woody-mode-$sessionKey.json"
    if (-not (Test-Path -LiteralPath $modeFile)) { exit 0 }
    $mode = Get-Content -LiteralPath $modeFile -Raw -Encoding UTF8 | ConvertFrom-Json
    if (-not $mode.enabled) { exit 0 }

    # Что считается попыткой выхода из самого режима: вызов mode.ps1 с -Off, прямая
    # правка или удаление файла режима. Список намеренно узкий и названный: широкий
    # запрет ловил бы чужое.
    $isExit = $false
    if ($cmd -match 'mode\.ps1' -and $cmd -match '-Off')              { $isExit = $true }
    if ($cmd -match 'woody-mode-[^\s''"|>]*\.json' -and $cmd -match 'Set-Content|Out-File|Remove-Item|del\s|rm\s') { $isExit = $true }
    if (-not $isExit) { exit 0 }

    # Approval существует — этого достаточно, чтобы ПРОПУСТИТЬ вызов дальше: сверку
    # session_id и одноразовое погашение файла делает сам mode.ps1 -Off, второй рубеж.
    if (Test-Path -LiteralPath $approval) { exit 0 }

    $why = "Выход из режима оркестрации Дятла Вуди без согласования ЛПР запрещён. " +
           "Режим включён для сессии $($mode.session_id). " +
           "Согласование — файл $approval, который кладёт ЛПР, а не сессия: " +
           "сессия, выписывающая себе разрешение, ничего не согласовала. " +
           "Если режим действительно надо снять — спроси ЛПР строкой «От тебя жду», " +
           "а не выключай молча."

    $json = @{
        hookSpecificOutput = @{
            hookEventName            = 'PreToolUse'
            permissionDecision       = 'deny'
            permissionDecisionReason = $why
        }
    } | ConvertTo-Json -Depth 4 -Compress

    # Байты в поток напрямую: PowerShell 5.1 отдаёт stdout в кодовой странице консоли,
    # и кириллица приходит как «???????». Поймано живым прогоном стража переменных.
    $out = [Console]::OpenStandardOutput()
    $bytes = [System.Text.Encoding]::UTF8.GetBytes($json)
    $out.Write($bytes, 0, $bytes.Length)
    $out.Flush()
    exit 2
}
catch {
    exit 0
}
