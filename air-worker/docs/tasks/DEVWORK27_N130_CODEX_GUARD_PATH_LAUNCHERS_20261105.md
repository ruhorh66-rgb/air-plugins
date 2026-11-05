# AirWorker N-130 · Codex: исправление P1 по путям оболочек

**Owner:** Codex, ручной запуск ЛПР. **Куратор:** Dev Work 27.
**Триггер:** независимый Anthropic Sonnet 5.5 `FAIL_P1 / RC1` на SHA `486ca875539faff79c9c1395ca2bd85ece758f54`.
**План:** действующий `air-worker/plan/N-130_rc4-release-p0-source-reviewer-fixes-and-transac.md` (N-130 OPEN, parent N-129); не создавать новый план.
**Git:** `ruhorh66-rgb/air-plugins`, ветка `dev/airworker-native-orch-pilot-20261104`. Код перед новым тестом: `486ca875539faff79c9c1395ca2bd85ece758f54`; после коммита RED-теста HEAD будет новее. Проверить реальный HEAD/CLEAN, не переписывать чужие изменения.
**Модель:** Codex `gpt6-astra:high` для защитной логики; независимый судья после реализации — Anthropic/Claude.
**Done_when:** реализовано распознавание настоящих команд при полном пути к оболочке, составных флагах и opaque PowerShell-запусках; новый RED-тест и все уже принятые тесты BypassGuard GREEN; `go vet`, `git diff --check`, commit/push/readback CLEAN.
**Judge:** `go test . -run '^(TestN130|TestN129NativeShellGuardBlocksWrongModels|TestСтражОбхода)'` + `go vet ./...` + Git-квитанция. Релизный judge N-130 позже, отдельно.
**Receipt:** точный SHA нового source-коммита, терминальные RC и результаты negative/positive cases.

## Реализовать — не ограничиваться анализом

Исправь `air-worker/cmd/bypassguard.go`, прежде всего `unwrapNativeShellLauncher` и связанный `classifyKnownShellWrapper`. Сейчас `first := strings.ToLower(strings.Trim(args[0], ...))` сравнивается с простыми `cmd/powershell/pwsh/bash/sh/zsh`: абсолютные пути и оболочки с составными опциями могут пройти мимо проверки.

1. **Полные пути к запускателям:** распознавать Windows `C:\Windows\System32\cmd.exe`, `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, цитируемый путь с пробелами `"C:\Program Files\PowerShell\7\pwsh.exe"`, POSIX `/bin/bash`, `/usr/bin/bash`, `/bin/sh`. Учитывай различие разделителей `\\` и `/` и кавычки; не теряй аргументы после имени EXE. Нормализуй только известные программы-оболочки, не произвольные исполняемые файлы.
2. **Bash/POSIX:** обрабатывай составные флаги с `c`: `-ec`, `-xc`, `-lc` и комбинации, содержащие команду, а не только точные `-c/-lc/-ic`. Исполняемый payload проверяй существующим стражем; разрешённые `git status`, `echo` не блокируй без причины.
3. **PowerShell:** `-EncodedCommand` — непрозрачная исполняемая команда; не пропускай её без проверки. При невозможности доказать безопасность — fail-closed. Аналогично рассмотри `-File` для внешних скриптов; не делай необоснованный общий ALLOW. Не декодируй/не выполняй содержимое при проверке и не обращайся к произвольным внешним скриптам. Добавь отрицательные тесты, если реализуешь дополнительные варианты.
4. **Сохранить прежний фикс:** `npx @anthropic-ai/claude-code` и `env -u/-C/-S/--` уже исправлены Codex в `486ca875`, их не переписывать без доказанной причины. Не ослаблять marketplace guard, самостоятельный Claude/Codex или семантический judge.

## Проверка

В `air-worker/cmd/bypassguard_test.go` уже добавлен `TestN130AnthropicP1PathQualifiedAndOpaqueShellLaunchers`: на исходном `486ca875` он **RED / RC1**, воспроизводит 11 незаблокированных вариантов и содержит 7 разрешённых команд. Не удаляй и не меняй ожидаемые результаты ради GREEN. Уточнять тест можно только если обнаружено доказуемое несоответствие реальному синтаксису оболочки.

После исправления: `gofmt` изменённых Go-файлов; `go -C air-worker/cmd test . -run '^(TestN130|TestN129NativeShellGuardBlocksWrongModels|TestСтражОбхода)' -count=1 -timeout 180s`; `go -C air-worker/cmd vet ./...`; `git diff --check`. `GOTMPDIR` — только для собственного процесса на D:, если унаследованное R: не подходит. Результаты записать с реальным exit code; `SKIP` — не PASS.

Коммитить только разрешённые изменения в `bypassguard.go` и `bypassguard_test.go` после проверки состояния Git. Push в существующую ветку, подтвердить local=upstream=remote SHA и CLEAN. Возвратить краткую квитанцию и SHA. Если штатный инструмент откажет в изменении — остановить отклонённую операцию и сообщить буквальный отказ; не обходить ограничение через другой инструмент, файл или исполнителя.

**Не делать:** установку или публикацию rc.4, UAC, reboot, остановку чужих служб, замену AirWorker rc.2, прямой вызов Claude/Codex вне штатных маршрутов AirWorker, правку нативных `PLAN.md/plan/N-*/LEARN` вручную. После GREEN понадобится отдельный независимый Anthropic source/security review на финальном SHA и полный релизный гейт N-130.

**Критерий сдачи этой задачи — изменённый работающий код с GREEN, не аналитическая записка.**
