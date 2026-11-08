# AirWorker · ChatGPT-координатор и два самостоятельных Codex-кодера

**Класс:** операционная процедура собственного самообучения AirWorker, Dev Work 30, 08.11.2026. Это не разрешение на установку или закрытие P0.

## Trigger

В обычной беседе ChatGPT пользователь поручил продолжить разработку AirWorker в режиме двух Codex-воркеров.

## Procedure

1. **Координатор — текущая беседа ChatGPT.** Читает существующий PLAN и открытые N-узлы, нативные квитанции и текущие `git HEAD/status/upstream`; объявляет собственную session identity. Не использует старую session key для нового запуска.
2. **Два Codex-воркера физически пишут код**, каждый в собственном отдельном Git worktree, с нативным `workspace-write` и запечатанным job receipt. Штатный source-only путь: `air-worker orchestrate code -product <root> -task-file <TASK> -principal gpt -session-key <own-session> -tier <approved-tier> -json`. Нет третьего Codex-лидера; модельный текстовый diff без файлов не является кодингом.
3. Координатор сверяет `DONE + exit_code=0`, роли `coding-subagent/worker`, исходный SHA, два `git diff HEAD` и их SHA, разрешённые файлы, отсутствие изменений в исходной ветке и `git apply --check` обоих результатов. Затем **координатор**, а не модель-лидер, последовательно объединяет изменения и выполняет factual Go QA, `gofmt`, `go vet` и независимый Anthropic Sonnet/Opus judge на точном SHA.
4. **Обрыв/неизвестное исполнение:** `air-worker orchestrate code status` для того же `session+TASK` читает существующий `RUNNING/DONE/FAILED/EXECUTION_UNKNOWN`; никогда не инициирует вторую модельную задачу. При `EXECUTION_UNKNOWN` сначала exact readback `job_id/args/SHA/exit/recovery_ref`. Два настоящих межчатовых разрыва ещё нужны для G07; имитации и unit QA недостаточно.
5. **Эскалация по причине**, а не стоимостью: `script → GPT-6 Luna → GPT-6.1 Terra → GPT-6 Sol → GPT-6.1 Sol → GPT-6 Astra`, каждый low/medium/high, переход только при доказанном реальном отказе/застое того же TASK и SHA. Synthetic vendor_limit демонстрирует логику, но не даёт G06 PASS.
6. **Границы приёмки:** handoff `READY_FOR_CHATGPT_COORDINATOR_INTEGRATION`, модельное `DONE`, source-only тест или семантический текст без структурированного JSON не закрывают P0. G05 — финальные QA + независимые Anthropic и Codex Sol low + обратимый rollback; G08 — отдельное нативное закрытие N123/N126/N136/N139. Принятые G01–G04 не прогонять повторно.
7. **Релиз отделён:** установлен AirWorker `0.11.11-rc.2`, `0.11.11-rc.5` — source candidate/HOLD. Предыдущая dist B `a96e2a2` устарела; релиз требует новый clean S→B→R с подписью и отдельный install gate. Не менять чужие процессы, ACL или GIT.

## Pitfalls

- Два read-only консультанта + третий Codex-писатель — **старый, не согласованный пользователем режим**; не возвращать его.
- Два последовательных `executor dispatch` — не два параллельных Codex-кодера под ChatGPT-координацией.
- Не выдавать `NOT_PROVEN`, `RUNNING` или синтетические тесты G07/G06 за закрытые гейты.
- После обрыва сначала читать сохранённую native receipt, не повторять модельный процесс.
- Чистить только собственное Git-дерево, не удалять чужие worktrees/R:-runtime и принятые receipts.

## Evidence

- Два действительных кодирующих воркера и фактическая интеграция: `docs/receipts/DEVWORK30_REAL_AIRWORKER_CHATGPT_TWO_CODERS_20261108.json`.
- G07 recovery source-only: `docs/receipts/DEVWORK30_P0_G07_RECOVERY_SOURCE_ONLY_20261108.json` — **G07 OPEN**.
- G06 causal source-only: `docs/receipts/DEVWORK30_P0_G06_CAUSAL_SOURCE_ONLY_20261108.json` — **G06 OPEN**.
- G05 structured judge source-only: `docs/receipts/DEVWORK30_P0_G05_STRUCTURED_CLAUDE_SOURCE_ONLY_20261108.json` — **G05 OPEN**.
- Источник последней правды: `git rev-parse HEAD` и `tools/p0-progress.ps1`, не исторические заголовки.
