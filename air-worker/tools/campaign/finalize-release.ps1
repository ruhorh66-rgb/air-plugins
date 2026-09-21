# After the campaign: a clean release repository with a three-commit history on top of the published 0.10.11 (adfb7ca).
#   S - sources, tests, registry data, campaign files (everything of the working branch except binaries, CHANGELOG, receipt)
#   B - binaries built from a clean checkout of S, with the flags of the published build (-trimpath; -s -w; tray -H=windowsgui)
#   R - CHANGELOG entry and acceptance receipt with the hashes of B
# The working branch keeps its intermediate commits (some carry binaries built with a wrong flag set); they are not published.
# Result: .campaign/release-repo, branch aworker/air-worker-0.10.11-fix-release. Nothing is pushed. Run with pwsh 7.
param([string]$BaseRev = 'adfb7cac8bc5bbe6e5318490603499a9fa6f4fca')
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'lib.ps1')
$root = $script:Root
$wt = (& git -C $root rev-parse --show-toplevel).Trim()
$id = @('-c', 'user.name=Aworker', '-c', 'user.email=aworker@srvlm01.local')
$branch = 'aworker/air-worker-0.10.11-fix-release'
$rel = Join-Path $root '.campaign\release-repo'
$tmp = Join-Path $root '.campaign\release-clean'
$out = Join-Path $root '.campaign\release-out'
foreach ($d in @($rel, $tmp, $out)) { Remove-Item -LiteralPath $d -Recurse -Force -ErrorAction SilentlyContinue }
if ((& git -C $wt status --porcelain -- air-worker) ) { throw 'working branch has uncommitted changes under air-worker: commit them first' }
$head = (& git -C $wt rev-parse HEAD).Trim()

# S
& git clone -q -c core.autocrlf=false --no-checkout $wt $rel
& git -C $rel checkout -q -b $branch $BaseRev
& git -C $rel checkout -q $head -- air-worker
& git -C $rel checkout -q $BaseRev -- air-worker/bin air-worker/CHANGELOG.md
& git -C $rel rm -q -f --ignore-unmatch air-worker/docs/receipts/AIRWORKER_RELEASE_0.10.11_FIX_ACCEPTANCE_2026-09-21.md
& git -C $rel @id commit -q -m 'fix(air-worker): close verification defects D-1..D-5 in the 0.10.11 line (sources, tests, registry, campaign files)'
$src = (& git -C $rel rev-parse HEAD).Trim()
Write-Output ('S: ' + $src)

# B
& git clone -q -c core.autocrlf=false --no-checkout $rel $tmp
& git -C $tmp -c advice.detachedHead=false checkout -q $src
New-Item -ItemType Directory -Force -Path $out | Out-Null
$env:CGO_ENABLED = '0'
$mod = Join-Path $tmp 'air-worker\cmd'
& go -C $mod build -trimpath '-ldflags=-s -w' -o (Join-Path $out 'air-worker.exe') .
if ($LASTEXITCODE -ne 0) { throw 'go build air-worker failed' }
& go -C $mod build -trimpath '-ldflags=-s -w -H=windowsgui' -o (Join-Path $out 'air-worker-tray.exe') ./tray
if ($LASTEXITCODE -ne 0) { throw 'go build tray failed' }
foreach ($n in @('air-worker.exe', 'air-worker-tray.exe')) {
    Copy-Item -LiteralPath (Join-Path $out $n) -Destination (Join-Path $rel ('air-worker\bin\' + $n)) -Force
    $info = (& go version -m (Join-Path $rel ('air-worker\bin\' + $n)) | Out-String)
    if ($info -notmatch ('vcs\.revision=' + $src)) { throw ('vcs.revision of ' + $n + ' is not S') }
    if ($info -notmatch 'vcs\.modified=false') { throw ('vcs.modified is not false in ' + $n) }
}
& git -C $rel add air-worker/bin/air-worker.exe air-worker/bin/air-worker-tray.exe
& git -C $rel @id commit -q -m ('build(air-worker): package binaries built from a clean checkout of ' + $src.Substring(0, 7))
Write-Output ('B: ' + (& git -C $rel rev-parse HEAD))

# R
$exeP = Join-Path $rel 'air-worker\bin\air-worker.exe'; $trayP = Join-Path $rel 'air-worker\bin\air-worker-tray.exe'
$h1 = (Get-FileHash -Algorithm SHA256 -LiteralPath $exeP).Hash.ToLower(); $s1 = (Get-Item $exeP).Length
$h2 = (Get-FileHash -Algorithm SHA256 -LiteralPath $trayP).Hash.ToLower(); $s2 = (Get-Item $trayP).Length
$ver = (& $exeP version | Out-String).Trim()
function Read-Result([string]$Name) { Get-Content -Raw -LiteralPath (Join-Path $script:ResDir ($Name + '.json')) | ConvertFrom-Json }
$suite = Read-Result 'suite'; $stand = Read-Result 'stand'; $m6 = Read-Result 'm6'; $c7 = Read-Result 'c7'; $d5m = Read-Result 'd5m'
foreach ($r in @($suite, $stand, $m6, $c7, $d5m)) { if (-not $r.ok) { throw ('result ' + $r.check + ' is not green') } }
$entry = @'
### Corrections after independent verification (2026-09-21)

- Receipt writes on Windows retry `os.Rename` while another process holds the target open, so the PID of a launched process is no longer lost when `adapter status` reads the receipt at the same moment.
- A failed PID registration after launch is no longer swallowed: the process is stopped and the launch fails with a named reason instead of leaving an invisible semantic reviewer.
- A test now fails when the semantic reviewer is launched without a receipt; the mutation check confirms it.
- The registry reader accepts the `facts` key as `items`; the product `goals` answer yes again.
- A static guard fails on any Windows PowerShell launch outside the sanitizing helper.
- The core closes a model step in `PLAN.md` itself, only after both judges pass; any edit of `PLAN.md` by the executor is rejected and the file is restored byte for byte.
- Release binaries are built from a clean checkout of the source commit with the flags of the published build; the embedded revision is that commit and `vcs.modified` is `false`.

'@
$chPath = Join-Path $rel 'air-worker\CHANGELOG.md'
$ch = [System.IO.File]::ReadAllText($chPath, [System.Text.Encoding]::UTF8)
$ch = [regex]::Replace($ch, '(?m)^## 0\.10\.10 ', ($entry.TrimEnd() + "`n`n## 0.10.10 "), 1)
[System.IO.File]::WriteAllText($chPath, $ch, (New-Object System.Text.UTF8Encoding($false)))
$rc = @"
# AirWorker 0.10.11 (исправленный) — квитанция приёмки кампании

Дата: $((Get-Date).ToString('yyyy-MM-dd HH:mm')) (SRVLM01)
Исходный коммит сборки (S): ``$src``
Версия бинарника: $ver
Исполнитель шагов с кодом: Codex (gpt-5.6-luna), смысловой судья: Claude. Оркестрация: Aworker.
Публикация, тег, Release, marketplace, установка: не выполнялись, требуют отдельного слова ЛПР.

## Артефакты

| Файл | Байт | SHA-256 |
|---|---:|---|
| ``bin/air-worker.exe`` | $s1 | ``$h1`` |
| ``bin/air-worker-tray.exe`` | $s2 | ``$h2`` |

## Результаты кампании

- Полный прогон (vet, go test, Python-тесты плагина, check-plugin): OK
- Стенд гонки квитанции: $($stand.detail)
- Мутация M6: $($m6.detail)
- Сценарии Ц7: $($c7.detail)
- Мутации Д-5: $($d5m.detail)

Независимая сверка v2 и подпись куратора — гейт 12 плана кампании.
"@
[System.IO.File]::WriteAllText((Join-Path $rel 'air-worker\docs\receipts\AIRWORKER_RELEASE_0.10.11_FIX_ACCEPTANCE_2026-09-21.md'), $rc, (New-Object System.Text.UTF8Encoding($false)))
& git -C $rel add air-worker/CHANGELOG.md air-worker/docs/receipts/AIRWORKER_RELEASE_0.10.11_FIX_ACCEPTANCE_2026-09-21.md
& git -C $rel @id commit -q -m 'docs(air-worker): changelog and acceptance receipt for the corrected 0.10.11'
Write-Output ('R: ' + (& git -C $rel rev-parse HEAD))
Write-Output ('release repo: ' + $rel + ' branch ' + $branch)
exit 0
