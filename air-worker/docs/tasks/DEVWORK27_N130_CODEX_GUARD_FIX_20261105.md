# ТЗ для штатного Codex — AirWorker N-130: исправить BypassGuard

**Исполнитель:** Codex, ручной запуск ЛПР. Предлагаемый режим: `gpt6-astra:high` (сложная защитная логика и отрицательные тесты).
**Продукт:** AirWorker, SRVLM01.
**План:** `plan/N-130_rc4-release-p0-source-reviewer-fixes-and-transac.md` — существующий **OPEN** N-130 (parent N-129). Не создавать новый план и не закрывать N-130 по результату этого задания.
**Git:** `ruhorh66-rgb/air-plugins`, ветка `dev/airworker-native-orch-pilot-20261104`.
**Проверенный входной SHA:** `3533f33e31cd61df700c32fb392e718fb0a55d86` (CLEAN на момент подготовки). Перед работой проверить реальный HEAD/изменения; при отличии — сверить факты, не сбрасывать чужую работу.

## Цель

**Реализовать исправление двух P1-дефектов в BypassGuard**, обнаруженных независимым Anthropic-review на source SHA `671eb927ae00d081edf6478960b649196a0f1553`. Не ограничиваться анализом или отчётом. Нужен исправленный Go-код, GREEN регрессия и проверяемый коммит.

## Что править

1. `air-worker/cmd/bypassguard.go`, `unwrapNativeShellLauncher`, ветка `case "npx", "npx.cmd"`: обработчик сейчас нормализует только `@openai/codex`. Добавить корректное распознавание `@anthropic-ai/claude-code` как запуска Claude, включая `npx -y` / `--yes`; передавать фактические аргументы в существующий контроль `classifyBypass`. Прямые вызовы `-p` / `--print` должны получать DENY, диагностический `--version` — ALLOW.
2. Тот же файл, ветка `case "env"`: сейчас пропускаются только переменные `NAME=VALUE` и `-i`. Корректно разбирать `-u / --unset`, `-C / --chdir` со значениями и формами `--unset=...`, `--chdir=...`, `-S / --split-string`, `--`, `-i`; проверять именно следующую исполняемую команду через уже существующий guard, не допуская скрытого `codex exec/e/app-server` или `claude -p/--print`. Сохранять разрешённые диагностические команды.
3. `air-worker/cmd/bypassguard_test.go`: **не ослаблять и не удалять** `TestN130AnthropicNativeWrapperNpxAndEnvOptions`. Сейчас воспроизведён `RED / RC1`, в тесте 10 реально пропускаемых запусков. Довести его до `PASS / RC0`; сохранить прежние `TestN130AnthropicShellWrappersMustNotHideVendorModelCalls`, `TestN130AdditionalAnthropicShellPrefixes`, `TestN130CompoundAuthorizedMarketplaceCannotMaskLaterBypass` и `TestN129NativeShellGuardBlocksWrongModels`.

## Судья и сдача

- `gofmt` только изменённых Go-файлов.
- Из Git-root выполнить адресную проверку (при необходимости задав **только своему процессу** `GOTMPDIR` на D:): `go -C air-worker/cmd test . -run '^(TestN130|TestN129NativeShellGuardBlocksWrongModels|TestСтражОбхода)' -count=1 -timeout 180s`.
- `go -C air-worker/cmd vet ./...` и `git diff --check`.
- Зафиксировать **фактические** exit codes, а не считать `DONE` модели за PASS. Ни один `SKIP` не выдавать за PASS.
- Если проверки GREEN — сделать source commit/push в указанную ветку после контроля отсутствия чужих изменений, подтвердить local/upstream/remote SHA и CLEAN; вернуть commit SHA, краткую дельту, результаты тестов и оставшиеся блокеры.
- **Не выполнять** подпись/публикацию/установку rc.4, менять установленную rc.2, UAC/reboot, останавливать службы или редактировать native `PLAN.md`, `plan/N-*`, LEARN вручную. N-130 остаётся OPEN до независимого Anthropic-review итогового SHA, полного применимого QA и релиза.

## Важная граница инструментов

В этой GPT-беседе была **отклонена до выполнения** операция `AIR_Commander_Test.edit_block` на `air-worker/cmd/bypassguard.go`. Причина отказа платформой не раскрыта. Это ТЗ для **самостоятельно запущенной ЛПР штатной сессии Codex**, а не разрешение обходить защиту. Если собственные действия Codex также блокируются защитой, **остановить отклонённую операцию и вернуть точный отказ**, не менять инструмент, канал, путь или исполнителя ради обхода.

## Исходные факты / дальнейший маршрут

- Код до исправления: `3533f33`, без правки `bypassguard.go` после RED.
- Последний независимый Anthropic: `FAIL_P1` (2 findings: `npx @anthropic-ai/claude-code`, `env` options).
- Подробный контекст: AirWorker Wiki `02_RESULTS/DEVWORK27_N130_PLATFORM_DENIAL_QA_VCS_DIAGNOSTIC_20261105.md`.
- После фикса Codex: независимый Claude source-review **на точном новом SHA** → полная применимая QA → VCS-stamped CLI/Tray → проверки Claude/Codex/Hermes → штатный подписанный релиз и проверка отката. Этот последующий этап не входит в текущую правку.

**Результат этой задачи:** реализованные изменения в двух Go-файлах + GREEN и Git-квитанция; не один лишь совет по исправлению.
