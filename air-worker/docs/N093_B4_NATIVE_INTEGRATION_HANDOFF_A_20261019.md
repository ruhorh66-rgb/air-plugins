# AirWorker N-093 — линия A / последовательная нативная интеграция B4

**19.10.2026 SGT · состояние: N-093 CLOSED в отдельной integration-ветке A; в исходной рабочей ветке B узел по-прежнему OPEN.**

## Подтверждённые входные гейты B

Линия B передала подписанную [B4-квитанцию](receipts/N093_B4_SINGLE_WRITER_ACCEPTANCE_20261019.json) SHA256 `e18fa530c3a6affed06816cbc8f0712125712fdbb59e0c2a04e3d0acb5fecdbf`. Независимый `gpt-6-astra high` по source `752a249`: **PASS, P0/P1=0**. Реальная процедура дала `procedure_used=1`, `deferred_missing_configuration` и предотвращение лишней валидации, но не успешную полную валидацию. B отдельно доказала native close в disposable copy: первый вызов, повтор, lint/validate — RC0.

Последний закреплённый source HEAD B `8bbe3e7d22d6253822b73969b84288bc03d73ea9` совпал с GitHub. На момент интеграционного шага B имела девять незакоммиченных файлов R03, без PLAN/N/LEARN.

## Выполнено ядром

От B HEAD `8bbe3e7` создан отдельный worktree `dev/airworker-n093-b4-native-close-a-20261019`; исходные SHA `PLAN.md`, N-093, LEARN и `plan/release-slices.json` точно совпали с квитанцией B.

Один вызов установленного **AirWorker 0.11.8**:

```text
air-worker plan node close N-093_p0-self-use-verifiable-outcome-for-newly-learned -receipt docs/receipts/N093_B4_SINGLE_WRITER_ACCEPTANCE_20261019.json -product <isolated product root>
```

**RC0**, N-093 получил **CLOSED**, управление вернулось к N-076. Количество открытых узлов: **40 → 39**. Ядро изменило ровно `PLAN.md`, `plan/N-093*.md`, `learn/events.jsonl` и добавило **одно** событие `LE-1a2e27d4d6bd5570b26d`. `release-slices.json` не изменён. Штатные `node list`, `spine`, `plan-lint`, `validate` — **RC0**.

Первичные тестовые SHA и контрольное состояние находятся в [машинной квитанции A](receipts/N093_B4_NATIVE_INTEGRATION_A_20261019.json). Журналы команд (вне продукта) — `R:/-4-/air-worker/work/N093_B4_SINGLE_WRITER_20261019/N093_A_NATIVE_INTEGRATION_20261019/`.

## Для воркера B и куратора Voody

**Не переносить вручную или Git cherry-pick только `PLAN.md`, `plan/N-*`, LEARN из этого worktree в рабочую ветку B.** В исходной B ветке узел не закрыт; её девять файлов R03 оставлены без изменений. После checkpoint R03 и сверки HEAD назначить одного writer и выполнить **штатное native core close** в выбранной рабочей ветке, с той же подписанной B4-квитанцией, затем lint/validate, один LEARN event, Git commit/readback. Интеграционная ветка A является проверенным результатом подготовки, не подтверждением release 0.11.9.

**Owner:** A — native close и факт/квитанция; B — R03 source development; curator — координация выбранной нити. **Trigger:** B4 high PASS. **Done when:** последовательное core-закрытие N093 принято в общей рабочей ветке с SHA/commit/receipt, без чужого WIP. **Judge:** native CLI и подписанный Astra high. **Rollback:** исходная ветка B и установленная AirWorker 0.11.8 не менялись; отдельную интеграционную ветку можно не принимать.

N-124 остаётся OPEN; R02/R03/R05 и релиз 0.11.9 требуют отдельных приёмок. Никаких release/tag/install, UAC/reboot, остановки служб не выполнялось.
