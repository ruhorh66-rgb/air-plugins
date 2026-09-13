<#
.SYNOPSIS
  Чтение журнала сессии: последнее сообщение ассистента и рабочий каталог хода.

.DESCRIPTION
  Задача 1, 12.09.2026. У Stop харнесс кладёт last_assistant_message прямо в событие.
  У UserPromptSubmit такого поля нет — событие наступает СО СЛЕДУЮЩИМ сообщением ЛПР,
  то есть уже после хода, который проверять поздно, и единственный источник текста —
  журнал сессии по transcript_path.

  Формат журнала выяснен ФАКТОМ 12.09.2026 чтением реального файла собственной сессии
  (E--5--014-Skills-air-woody\<session_id>.jsonl под каталогом projects харнесса): JSONL,
  построчно объект с полем верхнего уровня "type" ('assistant' | 'user' | ...), полем
  "cwd" (рабочий каталог на момент СТРОКИ, а не события) и, для ассистента, полем
  "message.content" — массивом блоков. У блока текста "type":"text","text":"...";
  соседние блоки бывают "thinking" и "tool_use" без текста вовсе. Наблюдалось вживую:
  одно сообщение ассистента может состоять только из мышления и вызова инструмента, и
  тогда искомый текст — в СЛЕДУЮЩЕЙ по счёту строке "assistant" перед строкой "user"
  (результат инструмента). Отсюда: последнее сообщение ассистента — это последняя
  строка типа "assistant", у которой ЕСТЬ хоть один текстовый блок, а не просто
  последняя строка типа "assistant" вообще.

  Документация UserPromptSubmit (code.claude.com/docs/en/hooks.md) называет session_id,
  transcript_path, cwd в общих полях события — но это подтверждено чтением документации,
  не прогоном живого события на этой машине: прогон случится только со следующим
  реальным сообщением ЛПР в сессии с включённым режимом. prompt-guard.ps1 поэтому пишет
  необработанный снимок stdin в woody-prompt-probe-<session_id>.json при КАЖДОМ запуске —
  не только «при первом», а всегда, но дёшево (обрезано) — чтобы факт был проверяемым
  снова, а не одноразовым наблюдением, которое никто не смог бы повторить.

  Обе функции пропускают молча (возвращают $null) при любой ошибке разбора — вызывающий
  хук сам решает, что означает «нечем узнать»: как правило, то же самое, что «страж
  молчит при собственной ошибке».
#>

function Get-LastAssistantText {
    param([string]$Path)

    if (-not $Path -or -not (Test-Path -LiteralPath $Path -PathType Leaf)) { return $null }
    $lines = $null
    try { $lines = Get-Content -LiteralPath $Path -Encoding UTF8 } catch { return $null }
    if (-not $lines) { return $null }

    for ($i = $lines.Count - 1; $i -ge 0; $i--) {
        $line = $lines[$i]
        if (-not $line -or -not $line.Trim()) { continue }
        $o = $null
        try { $o = $line | ConvertFrom-Json } catch { continue }
        if ([string]$o.type -ne 'assistant') { continue }
        $content = $o.message.content
        if (-not $content) { continue }
        $texts = @($content | Where-Object { [string]$_.type -eq 'text' } | ForEach-Object { [string]$_.text })
        if ($texts.Count -eq 0) { continue }
        return ($texts -join "`n")
    }
    return $null
}

function Get-EventCwd {
    param($Ev)

    if ($Ev.cwd) { return [string]$Ev.cwd }

    # Запасной путь: событие могло не принести cwd (не подтверждено прогоном на этой
    # машине — см. описание файла), а каждая строка журнала несёт СВОЙ cwd на момент
    # записи. Последняя строка -- ближайшее известное значение к текущему ходу.
    $tp = [string]$Ev.transcript_path
    if (-not $tp -or -not (Test-Path -LiteralPath $tp -PathType Leaf)) { return $null }
    try {
        $lines = Get-Content -LiteralPath $tp -Encoding UTF8
        for ($i = $lines.Count - 1; $i -ge 0; $i--) {
            if (-not $lines[$i] -or -not $lines[$i].Trim()) { continue }
            $o = $null
            try { $o = $lines[$i] | ConvertFrom-Json } catch { continue }
            if ($o.cwd) { return [string]$o.cwd }
        }
    } catch { }
    return $null
}
