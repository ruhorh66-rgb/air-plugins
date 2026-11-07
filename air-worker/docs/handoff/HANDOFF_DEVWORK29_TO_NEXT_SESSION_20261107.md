# AirWorker · Dev Work 29 → следующая сессия · 07.11.2026 SGT

**Статус: P0 OPEN · 4/8 критериев PASS, 4 осталось · 0/4 native nodes CLOSED.** Это HANDOFF_SOURCE_ONLY, а не приёмка всего P0, разрешение на release/install или признание двух реальных disconnect.

## 1. Решения ЛПР / ближайший измеримый результат

**Owner:** следующий воркер AirWorker на SRVLM01; куратор Voody/Vega получает блокеры по штатному ChatLink, только ЛПР принимает release/install/reboot/UAC/foreign-service гейты. **Trigger:** ЛПР 07.11.2026 «сохранить наработки, актуализировать планы, дерево должно быть чистым, подготовить handoff в новой сессии». Работа — строго по существующим PLAN.md и native plan/N, через кодирующую лестницу Codex и независимого Anthropic, без ручного второго orchestrator. **Done_when P0:** native real AirWorker PLAN→Codex code→factual Go PASS→Anthropic PASS→plan node close плюс доказанный reconnect + causal escalation на той же native машине, source SHA GitHub readback и exact immutable receipts. **Judge:** native run and plan receipts, targeted Go / full N123 QA на финальном SHA, independent Anthropic/Codex where required. **Receipt:** docs/receipts/DEVWORK29_N123_SOURCE_ONLY_HANDOFF_20261107.json и docs/receipts/DEVWORK29_P0_ACCEPTANCE_MATRIX_20261107.json.

**Полная метрика P0:** G01 machine goal PASS; G02 реальное Codex Luna coding N139 PASS; G03 bounded native factual Go PASS; G04 новый независимый Claude Sonnet verdict PASS по exact source+original receipts, при сохранении исходного NOT_PROVEN, PASS. **OPEN:** G05 final frozen source targeted/full QA+independent code/security judge+remote readback; G06 причина Luna→next tier подтверждена native job receipts; G07 два реальных forced message disconnect и EXECUTION_UNKNOWN с тем же durable job 0 duplicates; G08 четыре нативных узла полностью закрыты независимо. Native N-139/N-136/N-126/N-123 все OPEN. **Не менять метрику 4/8 по unit fixture.**

## 2. Где работать и что проверить первым

Только AIR Commander Test с явным `node=SRVLM01` (SRVLM01 сервер Франкфурт, время для ЛПР SGT). Source repo `D:\Devbox\AirWorker\worktrees\awlrn-orch-pilot-20261104`, product `...\air-worker`, branch `dev/airworker-native-orch-pilot-20261104`, GitHub `ruhorh66-rgb/air-plugins`. Wiki repo `F:\-5-\011_Plugins\AirWorker_Wiki`, branch `dev/airworker-curator-0.10.14`. **Финальные source/Wiki SHA получать `git rev-parse HEAD`, `@{upstream}`, `git status --porcelain=v1` и GitHub readback после handoff commit, не предполагать их из этого текста.** Не трогать другие trees, LEARN journals, чужие процессы, R: temp.

Начать в новой сессии: `air-worker version` (установленная 0.11.11-rc.2), `air-worker plan spine -product <product> -json`, `air-worker plan node list -product <product> -open -json`, `air-worker plan-lint -product <product> -json`; `powershell -NoProfile -ExecutionPolicy Bypass -File <product>\tools\p0-progress.ps1` (RC1 ожидаемо пока P0 OPEN). Объявить **новую собственную** native session `air-worker session declare -product <product> -principal gpt -session-key <new-own-session>`, не наследовать Dev Work29.

**Машинная цель:** `goal/operational-goal.json` — host-local, принадлежит native core, может ссылаться на SHA до последнего handoff commit. После чистого, проверенного Git HEAD выполнить только штатный `air-worker operational-goal advance -product ... -node N-139_n136-native-operational-goal-selector-for-real-a -from-head <verified previous goal pin> -source-head <verified current source HEAD> -principal gpt -session-key <new own session>` через source-only бинарник, если installed rc.2 не знает команду; не редактировать goal JSON вручную. Исторические `goal/goal.json` и `plan/release-slices.json` 0.11.9 R05 сохранить. **Важное:** не запускать новый Codex на том же N139 job identity, пока ядро не докажет отсутствующий live job и не даст новый корректный task/step.

## 3. Что уже принято, не повторять за деньги

- N-135 CLOSED: два реальных последовательных Codex coding jobs с independent Anthropic в изолированной fixture. N-136 disposable autonomous `PLAN→Codex→Go→Anthropic→close` доказан, на собственном AirWorker позднее выполнен полезный step, но N-136 OPEN до всего done_when.
- N139 на реальном AirWorker: native operational goal выбран и кодер Codex Luna low действительно изменил только cmd/job_receipt.go и cmd/job_status.go. Terminal Codex DONE `gpt__devwork29-7fc2da058a095e9a__1794026842523903500__3115cf12`; factual Go DONE `gpt__devwork29-7fc2da058a095e9a__1794027935412442500__30cad5a6`, Go output SHA `94efe696195b25b8e932f1dba57b71b15bc296e4524de8d536c3fea8fdbe652c`.
- Исходный Claude semantic `NOT_PROVEN` по противоречивому narrative Codex сохранён. Отдельный native `operational-goal reconcile` (без повторного Codex/Go) в новой native judge session дал Anthropic Sonnet semantic PASS, step_done=yes, drift=none, output SHA `19f34fe73e4ee545aa86da90bf5a0634ce4622621c698e3d538d6982bdf29c95`, receipt `docs/receipts/DEVWORK29_N139_G04_SEMANTIC_RECONCILE_20261107.json`. Это G04 PASS только для bounded source fix, не закрывает N139. Source code review read TASK and 3 exact Go citations, PASS_SOURCE_ONLY.
- Source GitHub commit до N123 WIP: `611e66df826ea274150a3cee2c798db05ff7a525`, тогда CLEAN; installed 0.11.11-rc.2 сохраняется. Native goal advance после последнего источника READBACK RC0. Не повторять rc.4 installer/tag, package S/B release N130 и ARP N138 отложены.

## 4. Dev Work29 N123 дельта сейчас — SOURCE_ONLY

Изменения:
1. `cmd/job_receipt.go`: source_head exact Git SHA when available, args_sha256 of executable+argv (не хранить секретные аргументы), process exit_code, recovery_ref receipt+job ID, optional deadline_utc; finalization только после Wait, DONE с nonzero отказан, оригинальный output seal/no-replay сохранён.
2. `cmd/job_status.go`: read-only status возвращает дополнительную machine provenance без повторного исполнения.
3. `cmd/job_recovery_p0_test.go`: два повторных reattach RUNNING в одной Go fixture 900ms с нулём дублей, конечная проверка того же job_id/status/exit=0/output SHA, negative exit=7, raw-arg privacy, invalid DONE.
**Новая отдельная адресная проверка после прежнего транспортного обрыва:** `go test . -count=1 -run '^(TestN123.*|TestCriterion42Pending|TestRunReceiptedFailsClosedWhenPIDRegistrationFails|TestN139TerminalOutputIntegrityRejectsTamper)$' -v` → RC0; `go vet .` → RC0; `gofmt -l` пусто; `air-worker plan-lint` RC0, `git diff --check` RC0. Exact файловые SHA — в `docs/receipts/DEVWORK29_N123_SOURCE_ONLY_HANDOFF_20261107.json`. **Исторический обрыв предыдущего shell не дал terminal RC, состояние этого старого вызова EXECUTION_UNKNOWN.** Проверки внутри одного Go процесса НЕ заменяют два реальных обрыва отдельных GPT сессий/чатов и полный N123 QA.
**N123 НЕ ЗАКРЫВАТЬ:** его done_when дополнительно требует два forced message disconnect, 1 EXECUTION_UNKNOWN, same job id/source SHA/args/exit/log+recovery_ref, отсутствие нового exec/убийства чужого процесса, full QA на финальном clean source, независимый Codex Sol low PASS и reversible rollback receipt.

## 5. Порядок дальнейшей работы

**G07 first:** воспроизвести два отдельных forced message disconnect и транспортный EXECUTION_UNKNOWN на одном собственном ограниченном долгом job с native receipt; читать тот же job в новой ChatGPT-сессии без повторного запуска и подтвердить единственный process start, terminal log SHA+exit+recovery_ref. Не создавать второй writer/модельную задачу ради теста. Если полнота G07 пока не доказана, оставить OPEN.

**G06 next:** на одном новом предназначенном native work item показать причинный tier-failure/stall→следующий Codex tier с неизменённым TASK и receipt ladder; не путать рукописную эскалацию с автоматической. N-126 требует его собственные sequential complex/bulk native routes и independent Anthropic judge; reuse N135 factual receipts where applicable, no redundant costly jobs.

**G05 last on final source:** полный требуемый N123 QA один раз на чистом точном HEAD (большой N124 suite ранее тайм-аут — не запускать бессмысленно), targeted regressions/go vet, независимый source judge. Получить GitHub exact commit readback. **G08:** только после полного PASS всех отдельных N123/N126/N136/N139 done_when выполнить native `plan node close` с подписанными квитанциями; ничего не редактировать вручную под `plan/N` или `learn/events.jsonl`.

## 6. P1, ChatLink и границы

P1 второй после P0. Живой отрицательный пример: ошибки N139, Go temp R: и semantic NOT_PROVEN **не превратились в самообучение**; 3 старых APPLIED правила `use_count=0`, общий R runtime CLI failures в статусе `unjudged`. Пример уже описан в PLAN.md / Wiki. Только позже штатный `CAPTURE→REVIEW→safe APPLY→new session USED→measured EFFECT`; не выдавать лог за обученный навык.

ChatLink Dev Work29 собственный `session_ref=7fc2da058a095e9a` (только историческая квитанция, новому воркеру нужен свой `chat_connect`); HELLO Vega на `7416487715ccb47d`. Dev ChatLink15 R1 один source-only read pass завершён: Bridge source ba73824, `chat_connect` content[] содержит ≤2 свежих pending, UNKNOWN отдельно, connect не auto ACK; reply `sm_34f494e9-5691-4f9a-8ae5-147807fd121a` на адрес `239b22d593279fd4` был pending на момент отправки, не считать доставленным без ACK. Не повторять эксперимент, не использовать Shadow Tasks/Scheduled Tasks или новый транспорт.

Границы: **никакой установки/нового релиза/tag, UAC, reboot, stop foreign services, чужого ACL, изменения GitHub security или CLI credentials без отдельного точного «да» ЛПР с rollback на installed rc.2**. Сохранённый source-only коммит не означает новый установленный продукт. План, код, обзоры и квитанции будут опубликованы в ветке, обе директории должны завершить CLEAN; эти условия проверь заново после нового SHA.

## 7. Минимальный readback после передачи

1. Сверить [канонический исходник](https://github.com/ruhorh66-rgb/air-plugins/tree/dev/airworker-native-orch-pilot-20261104/air-worker) и [Wiki](https://github.com/ruhorh66-rgb/AirWorker_Wiki/blob/dev/airworker-curator-0.10.14/00_STRATEGY/AIRWORKER_PRODUCTION_PLAN.md) со штатными `git rev-parse`/GitHub; оба дерева CLEAN, `plan-lint RC0`.
2. `air-worker version` установленная `0.11.11-rc.2`, native P0 `p0-progress.ps1` выдаёт 4 PASS / 4 OPEN, 0/4 CLOSED; source `N123` этот счётчик НЕ повышает.
3. Прочитать этот handoff, `docs/receipts/DEVWORK29_N123_SOURCE_ONLY_HANDOFF_20261107.json`, `docs/receipts/DEVWORK29_N139_G04_SEMANTIC_RECONCILE_20261107.json`, N123/N126/N136/N139. Не повторять accepted fixtures/Anthropic review без изменения SHA или доказанной необходимости.
