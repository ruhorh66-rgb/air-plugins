# AirWorker · handoff Dev Work 27 → следующая сессия · 05.11.2026 SGT

**Статус блока:** N-132 SESSION_SAVE · SOURCE_ONLY WIP PRESERVED; N-130 OPEN, RC4 NOT_RELEASED / NOT_INSTALLED. Это не приёмка релиза и не независимый Anthropic PASS.
**Owner:** Dev Work 27 / SRVLM01; следующая сессия принимает участок после Git и native PLAN readback.
**Trigger:** ЛПР 05.11.2026 SGT: «сохранить наработки, проверить дерево, обучить базу самообучения AirWorker, закоммитить, запушить, актуализировать планы, подготовить handoff».
**Done_when:** четыре новых LEARN-события записаны штатным ядром, фактические GREEN/RED разделены, source-only WIP/план и handoff сохранены, source+Wiki pushed with exact SHA/CLEAN; закрывается только N-132 после собственного judge/readback, N-130 остаётся OPEN.
**Judge:** `air-worker plan-lint`, `git diff --check`, real targeted test log+RC, LEARN native IDs and source/Wiki remote SHA, installed-runtime readback.
**Receipt:** `docs/receipts/DEV_WORK27_SESSION_SAVE_20261105.json`; source WIP commit `ac1a8f74fe281158f07ad0272404cfff4c74fb93`.

## 1. Точка входа в следующей сессии

- SRVLM01 — доступ только штатным **AIR Commander** с явным `node=SRVLM01`. Не использовать прямую машину, новые мосты или выдуманные CLI возможности.
- Source Git: `D:\Devbox\AirWorker\worktrees\awlrn-orch-pilot-20261104`, branch `dev/airworker-native-orch-pilot-20261104`, GitHub `https://github.com/ruhorh66-rgb/air-plugins`.
- Первый защищённый source-only WIP commit: `ac1a8f74fe281158f07ad0272404cfff4c74fb93`, local=origin verified CLEAN. Новая сессия **обязана прочитать актуальный HEAD**: handoff и закрытие N-132 могут добавить отдельные docs/PLAN commits после этого SHA.
- Продукт: `<source repo>\air-worker`; установленная версия `air-worker 0.11.11-rc.2` не менялась; Claude Code native `2.1.311`, readback PASS. Старый rc.2 — точка отката; не заменять его source кандидатом.
- Wiki: `F:\-5-\011_Plugins\AirWorker_Wiki`, branch `dev/airworker-curator-0.10.14`; исходный Wiki HEAD до этого handoff `9d0e100243e79d00a1eb895cc2e61d73e8cf7170`. Финальный Wiki HEAD прочитать из Git, не из памяти.
- Обязательный native ingress: `air-worker version`, `air-worker plan spine -product <product> -json`, `air-worker plan node list -product <product> -json`, `air-worker plan-lint -product <product>`, `air-worker learn self status -json` и фактический git status/upstream. Ядро `0.11.11-rc.2`; исходная rc.4 — только кандидат.
- По умолчанию использовать собственную identity/session-key новой сессии. Не присваивать session-key Dev Work 27 `DevWork27-e9f8c570c924d8d7` себе.

## 2. Нативная нить и продуктовый план

- `N-130_rc4-release-p0-source-reviewer-fixes-and-transac` — OPEN: завершить source-исправления и независимую проверку на точном SHA, VCS-stamped clean CLI+Tray, оба host cache + три plugin, официальный подписанный выпуск, readback установки/rollback rc.2.
- `N-129` parent OPEN; `N-126` native 2 Codex subagents + Anthropic independent reviewer OPEN; legacy `R0.11.9-05` не обходить; Hermes Self-Learning P0 только после рабочего rc.4.
- `N-131` Dev Work 26 handoff уже CLOSED, не открывать повторно.
- `N-132_dev-work-27-session-save-coding-lessons-and-new-s` — native child N-130 для текущего сохранения. Его можно закрыть **только после** commit/push/readback source+Wiki и квитанции N-132, не по готовности N-130.
- DEV-135 S0-first / микрорелиз, DEV-070 release gate, DEV-137 temp/cache lifecycle; отдельно избегать тяжёлой QA без новой дельты. Это один source-only handoff, не новый roadmap.

## 3. Точный WIP / RED / PASS

Файлы **кодинга**, сохранённые в `ac1a8f7`:
- `air-worker/cmd/bypassguard.go` — разбирает path-qualified/quoted Windows/POSIX shell launchers, Bash `-ec/-xc`, PowerShell opaque `-EncodedCommand/-File`, сохраняет ранее исправленные `npx @anthropic-ai/claude-code` и `env` опции.
- `air-worker/cmd/bypassguard_test.go` — `TestN130AnthropicP1PathQualifiedAndOpaqueShellLaunchers` теперь PASS; `TestN130PathShellOptionsFailClosedWithoutDiagnosticFalseDenials` пока FAIL **в двух** Bash случаях.

**Честный судья:** последняя адресная команда вернула `RC1`:
- `/usr/bin/bash -o pipefail -c 'claude --print review'` — прямой вызов всё ещё не заблокирован.
- `/usr/bin/bash -O extglob -ec 'codex exec --json'` — прямой вызов всё ещё не заблокирован.

Доказательство: `D:\Devbox\AirWorker\temp\DevWork27\guard-p1-20261105\path-guard-targeted.log`; SHA-256 `b3677726febee555a5639656c93849fcae6dc69f1c84c7bdfa3c0fdc77cfc3a3`. Не выдавать `go test ./...` или даже N-130 за PASS. В прошлой адресной проверке N-130/ N-129 guards / npx-env тесты PASS RC0 и **один Windows symlink test SKIP** (не PASS); этот результат относится к исходному WIP **до добавления** последнего Bash-теста. `go vet ./... RC0`, `git diff --check RC0`, `plan-lint RC0` на WIP без code release.
- Принятая предыдущая правка Codex: `486ca875539faff79c9c1395ca2bd85ece758f54`, `PASS_SCOPED_FIX`, но независимый Anthropic `FAIL_P1`; новая source delta `ac1a8f7` **ещё не проходила** независимый Claude-review.
- Никакого нового релиза, GitHub tag, signed Ed25519, UAC, reboot, service stop, изменения Claude/Codex/Hermes runtime, live hook installation на этом WIP не было.

## 4. Обучение AirWorker: записано ≠ применено

Четыре нативных `air-worker learn event -product <product> -actor-kind gpt-window -actor DevWork27...` вернули RC0 и создали журнал через **ядро**, не вручную:
1. `LR-20261105T130704Z-cbe8e9e6` — `shell-launcher-path-identity-and-quoted-argv`.
2. `LR-20261105T130705Z-50f118cf` — `bash-option-arity-and-explicit-red-gate`.
3. `LR-20261105T130705Z-24783bb1` — `fail-closed-powershell-opaque-with-diagnostic-fidelity`.
4. `LR-20261105T130705Z-d561dea9` — `source-only-handoff-preserve-red-and-exact-sha`.

Все четыре проверены чтением `air-worker/learn/events.jsonl`. `air-worker learn self status -json` = **enabled**, но selector owner указывает на исторический product root на R: (не D:). Нет доказательства auto-applied skill в следующей live session; не менять owner/selector вручную. Один native `learn review -product <D product> -session DevWork27-e9f8c570c924d8d7` завершился **review_error**: `reviewer did not return a successful agent_message`. Подтверждённая квитанция **ядра** `.air-worker/learn/reviews/RV-20261105T130958Z-56fb4586.json`, reviewer `codex:gpt-5.6-luna:medium`; исходный transcript пустой (SHA `e3b0c442...`). Обёртка PowerShell с ErrorAction Stop завершилась exit=1 до записи собственной дополнительной квитанции `native-learn-review.rc.json`; **её отсутствие не считать PASS или новой попыткой**. Четыре LEARN events записаны, но reviewed/applied skill = NOT_PROVEN. Следующей сессии **сначала readback native receipt/pending**, не повторный запуск без свежих данных и явной причины. Любой pending proposal проходит штатную review/approval/apply, не считать автоматическим применением.

## 5. Единственный следующий кодовый шаг

Довести `TestN130PathShellOptionsFailClosedWithoutDiagnosticFalseDenials` до GREEN: в `unwrapNativeShellLauncher` правильно учитывать Bash `-o` / `-O`, которые **потребляют следующий токен**, и затем обрабатывать последующий `-c` либо составное `-ec`. Добавить минимальный статический тест, если нужно, не ослаблять проверку прямого Codex/Claude и не превращать безопасный `git status` в DENY. При отказе конкретной операции — сохранить точный результат и продолжить только безопасное независимое.

После GREEN: native scoped `go test` + `go vet` с фактическим RC и immutable receipt, независимый Anthropic source/security review на CLEAN финальном SHA; затем применимая полная QA и VCS-stamped clean Git clone release build, официальный signed update и проверка CLI/Tray + Claude/Codex/Hermes/rollback rc.2. До этого N-130 OPEN.

## 6. Нельзя по этому handoff

- Не считать `N-132 CLOSED` доказательством `N-130` релизной готовности.
- Не изменять нативные `PLAN.md`, `plan/N-*`, `learn/events.jsonl` в обход AirWorker core.
- Не менять установленный rc.2, signing key, live profiles, чужие службы или UAC без точного разрешённого релизного шага.
- Не повторять долгий native job при `EXECUTION_UNKNOWN`: читать receipt/PID/HEAD и сохранённый код.
- Git WIP сохранён как источник для продолжения, не как автоматическое разрешение на выпуск.
