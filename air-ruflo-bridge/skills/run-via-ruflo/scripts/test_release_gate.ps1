# Проверка допущений, на которых стоят два новых гейта run_task.ps1:
# свежесть файла метрик и хвост в рабочем дереве (ERR-2026-000236).
# Проверяется не переписанная логика, а поведение самих команд на этой машине —
# именно оно и может обмануть гейт.
# 'Continue', а не 'Stop': git пишет в stderr при нормальной работе (например
# «not a git repository»), и под 'Stop' это стало бы исключением вместо ответа.
# Судим по $LASTEXITCODE и выводу — ровно как сам гейт. Проверки всё равно
# роняют прогон через throw.
$ErrorActionPreference = 'Continue'
$n = 0
function ok([string]$label, [bool]$cond) {
    if (-not $cond) { throw "ПРОВАЛ: $label" }
    $script:n++
    Write-Host "ok   $label"
}

$root = Join-Path ([System.IO.Path]::GetTempPath()) ("relgate-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Path $root | Out-Null
try {
    # 1. Не репозиторий: гейт обязан это опознать и промолчать, а не упасть.
    & git -C $root rev-parse --git-dir *> $null
    ok "вне репозитория rev-parse возвращает не ноль" ($LASTEXITCODE -ne 0)

    & git -C $root init -q 2>$null
    & git -C $root config user.email "test@example.com"
    & git -C $root config user.name "test"
    Set-Content -Path (Join-Path $root 'a.txt') -Value 'первая строка' -Encoding UTF8
    & git -C $root add a.txt 2>$null
    & git -C $root commit -q -m "первый" 2>$null

    & git -C $root rev-parse --git-dir *> $null
    ok "внутри репозитория rev-parse возвращает ноль" ($LASTEXITCODE -eq 0)

    $clean = (& git -C $root status --porcelain --untracked-files=no | Out-String).Trim()
    ok "чистое дерево даёт пустой porcelain" ($clean -eq '')

    # 2. Чужой НОВЫЙ файл не должен валить прогон — это и есть ложный отказ,
    #    ради которого взят --untracked-files=no.
    Set-Content -Path (Join-Path $root 'чужой.md') -Value 'файл параллельной сессии' -Encoding UTF8
    $stillClean = (& git -C $root status --porcelain --untracked-files=no | Out-String).Trim()
    ok "неотслеживаемый файл не считается хвостом" ($stillClean -eq '')
    $others = (& git -C $root ls-files --others --exclude-standard | Out-String).Trim()
    ok "но он виден отдельной строкой как замечание" ($others -ne '')

    # 3. Своя незакоммиченная правка — обязана быть видна.
    Add-Content -Path (Join-Path $root 'a.txt') -Value 'дописано и не закоммичено'
    $dirty = (& git -C $root status --porcelain --untracked-files=no | Out-String).Trim()
    ok "правка отслеживаемого файла видна как хвост" ($dirty -ne '')

    # 4. Свежесть: файл прошлого прогона старше старта, свой — не старше.
    $stale = Join-Path $root 'metrics-old.json'
    Set-Content -Path $stale -Value '{}' -Encoding UTF8
    (Get-Item $stale).LastWriteTimeUtc = [DateTime]::UtcNow.AddHours(-6)
    $startedAt = [DateTime]::UtcNow
    ok "файл от прошлого прогона опознаётся старым" ((Get-Item $stale).LastWriteTimeUtc -lt $startedAt)

    $fresh = Join-Path $root 'metrics-new.json'
    Set-Content -Path $fresh -Value '{}' -Encoding UTF8
    ok "файл этого прогона проходит по свежести" ((Get-Item $fresh).LastWriteTimeUtc -ge $startedAt)
} finally {
    Remove-Item -LiteralPath $root -Recurse -Force -ErrorAction SilentlyContinue
}
Write-Host "`n$n проверок пройдено"
