# Plan step 11: CHANGELOG entry and acceptance receipt with the results of this campaign, then a docs-only commit.
# Run with pwsh 7 (UTF-8, Cyrillic text).
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'lib.ps1')
$root = $script:Root
$repo = (& git -C $root rev-parse --show-toplevel).Trim()
Set-Location $repo
$id = @('-c', 'user.name=Aworker', '-c', 'user.email=aworker@srvlm01.local')
function Read-Result([string]$Name) { Get-Content -Raw -LiteralPath (Join-Path $script:ResDir ($Name + '.json')) | ConvertFrom-Json }
$suite = Read-Result 'suite'; $stand = Read-Result 'stand'; $m6 = Read-Result 'm6'; $c7 = Read-Result 'c7'
foreach ($r in @($suite, $stand, $m6, $c7)) { if (-not $r.ok) { throw ('result ' + $r.check + ' is not green') } }
$exeP = Join-Path $root 'bin\air-worker.exe'; $trayP = Join-Path $root 'bin\air-worker-tray.exe'
$info = (& go version -m $exeP | Out-String)
$rev = [regex]::Match($info, 'vcs\.revision=([0-9a-f]{40})').Groups[1].Value
$h1 = (Get-FileHash -Algorithm SHA256 -LiteralPath $exeP).Hash.ToLower(); $s1 = (Get-Item $exeP).Length
$h2 = (Get-FileHash -Algorithm SHA256 -LiteralPath $trayP).Hash.ToLower(); $s2 = (Get-Item $trayP).Length
$ver = (& $exeP version | Out-String).Trim()

$entry = @'
### Corrections after independent verification (2026-09-21)

- Receipt writes on Windows retry `os.Rename` while another process holds the target open, so the PID of a launched process is no longer lost when `adapter status` reads the receipt at the same moment.
- A failed PID registration after launch is no longer swallowed: the process is stopped and the launch fails with a named reason instead of leaving an invisible semantic reviewer.
- A test now fails when the semantic reviewer is launched without a receipt; the mutation check confirms it.
- The registry reader accepts the `facts` key as `items`; the product `goals` answer yes again.
- A static guard fails on any Windows PowerShell launch outside the sanitizing helper.
- Release binaries are built from a clean checkout of the source commit; the embedded revision is that commit and `vcs.modified` is `false`.

'@
$ch = [System.IO.File]::ReadAllText((Join-Path $root 'CHANGELOG.md'), [System.Text.Encoding]::UTF8)
if ($ch -notmatch 'Corrections after independent verification') {
    $ch = [regex]::Replace($ch, '(?m)^## 0\.10\.10 ', ($entry.TrimEnd() + "`n`n## 0.10.10 "), 1)
    [System.IO.File]::WriteAllText((Join-Path $root 'CHANGELOG.md'), $ch, (New-Object System.Text.UTF8Encoding($false)))
}

$rc = @"
# AirWorker 0.10.11 (исправленный) — квитанция приёмки кампании

Дата: $((Get-Date).ToString('yyyy-MM-dd HH:mm')) (SRVLM01)
Исходный коммит сборки: ``$rev``
Версия бинарника: $ver
Исполнитель шагов с кодом: Codex (gpt-5.6-luna), смысловой судья: Claude. Оркестрация: Aworker.
Публикация, тег, Release, marketplace, установка: не выполнялись, требуют отдельного слова ЛПР.

## Артефакты

| Файл | Байт | SHA-256 |
|---|---:|---|
| ``bin/air-worker.exe`` | $s1 | ``$h1`` |
| ``bin/air-worker-tray.exe`` | $s2 | ``$h2`` |

## Результаты

- Полный прогон (vet, go test, Python-тесты плагина, сборка кандидата, check-plugin): OK ($($suite.at))
- Стенд гонки квитанции: $($stand.detail)
- Мутация M6: $($m6.detail)
- Сценарии Ц7: $($c7.detail)

Независимая сверка v2 и подпись куратора — следующий шаг плана (гейт).
"@
$rp = Join-Path $root 'docs\receipts\AIRWORKER_RELEASE_0.10.11_FIX_ACCEPTANCE_2026-09-21.md'
[System.IO.File]::WriteAllText($rp, $rc, (New-Object System.Text.UTF8Encoding($false)))
& git add air-worker/CHANGELOG.md air-worker/docs/receipts/AIRWORKER_RELEASE_0.10.11_FIX_ACCEPTANCE_2026-09-21.md
& git @id commit -q -m 'docs(air-worker): changelog and acceptance receipt for the corrected 0.10.11'
Write-Output ('receipt committed: ' + (& git rev-parse --short HEAD))
exit 0
