# Базовые падения тестов rc.5 · долг канонической линии · 15.11.2026

Исполнитель: Dev Work 35. Получатель: writer канонической D-линии AirWorker.

> **Подтверждено чистым прогоном 15.11.2026 22:22–23:25** (короткий `GOTMPDIR`
> `R:\-4-\air-worker\cache\t-<tag>`, тот же Go и машина): **окружение, 43 из 44.**
> База `7e71303`: `go vet` 0, упал 1 тест — `TestSemanticReviewerRegistersReceipt`
> (тайминговый флейк, при -count=10 проходит 6/10), 1856 с. Связка А1+А2
> (`qa/airworker-a1-a2-on-rc5` = `a60e01c`): `go vet` 0, упал 1 тест —
> `TestJudgeSourceIdentityRejectsHiddenIndexFlags/assume-unchanged` (разовый сбой
> `git ls-files -v -z` с кодом 1 под нагрузкой; отдельно -count=5 — 5/5 PASS и на базе,
> и на связке), 1893 с. Долга в 44 теста у канонической линии нет; остаются два
> флейка. Таблица ниже — исторический первый прогон с длинным путём.

Прогон: `go vet . && go test -count=1 -timeout 60m ./...` в `air-worker/cmd` на
`7e7130399cc833e0921f746a77ee31ebb6502254` (исходник установленной 0.11.11-rc.5),
SRVLM01, Go 1.26.8, 15.11.2026 17:03–17:23, 1209 с. `go vet` — 0. Упало 44 теста.
Тот же набор (плюс два тайминговых флейка) упал на кандидате А1 `0ec419b`, то есть
это не следствие А1.

**Окружение прогона.** `GOTMPDIR` — каталог сессии длиной 104 символа
(`R:\-4-\temp\airhr\claude\R---4--claude-home-spares-spare-…\scratchpad\gotmp`);
к нему тесты добавляют свои подкаталоги и git-фикстуры. Поэтому большая часть
падений — превышение длины пути, а не код. Классификация ниже сделана по первой
строке отказа каждого теста (эвристика) и **подлежит подтверждению** повторным
полным прогоном с коротким `GOTMPDIR` (например `R:\-4-\air-worker\cache\gotmp`).

## Сводка

| Класс | Число | Что сделать |
|---|---|---|
| Окружение: длинный путь (`Filename too long`) | 30 | Повторить с коротким `GOTMPDIR`; если останется — тесту нужен `core.longpaths` в фикстуре |
| Окружение (вероятно): git-фикстура/рабочие деревья координатора (`NOT_PROVEN`, CRLF-предупреждения) | 8 | Повторить с коротким путём; затем разбирать как код |
| Код или фикстура: нужен разбор | 4 | Разбор writer'ом линии |
| Окружение: нет фикстуры Windows junction без прав | 1 | Требует непривилегированного junction; на SRVLM01 нет |
| Тайминг: гонка процесса и квитанции (флейк) | 1 | 10 запусков: rc.5 6/10, кандидат 8/10 |

## Список

| Тест | Класс | Первая строка отказа |
|---|---|---|
| TestChatGPTCoordinatorTwoCodexWorkersOwnRealEdits | окружение (вероятно): git-фикстура | orchestrate_coding_workers_test.go:1000: independent coding workspaces not proven (NOT_PROVEN) |
| TestCodingEscalation | окружение (вероятно): git-фикстура | orchestrate_coding_workers_test.go:1258: automatic original evidence EXECUTION_UNKNOWN |
| TestCodingMicrostepDispatch | окружение (вероятно): git-фикстура | orchestrate_coding_workers_test.go:1121: out-of-scope edit accepted (NOT_PROVEN) |
| TestCodingPartialPreparationCleanupAndRetention | длинный путь | coding_unstarted_cleanup_test.go:61: cannot create independent Git worktree 1 |
| TestCodingWorkersStatusRecovery | окружение (вероятно): git-фикстура | orchestrate_coding_workers_test.go:113: independent coding workspaces not proven |
| TestCodingWorkersStatusRecoveryLongIdentity | окружение (вероятно): git-фикстура | orchestrate_coding_workers_test.go:113: independent coding workspaces not proven |
| TestDW32IntegrationNestedProduct | окружение (вероятно): git-фикстура | orchestrate_coding_workers_test.go:1934: handoff NOT_PROVEN |
| TestDW32N139BoundCoordinatorAck | окружение (вероятно): git-фикстура | orchestrate_coding_workers_test.go:2045: handoff NOT_PROVEN |
| TestDW32StructuredBridgeDispatch | окружение (вероятно): git-фикстура | structured_coding_bridge_test.go:40: orchestration_not_proven |
| TestLegacyNonGoConcurrentCrossModeRefusal | код/фикстура | judge_unstarted_test.go:323: refusal changed … |
| TestN124PlanRephaseAppliesOneNodeOneReleaseAndReplaysIdempotently | длинный путь | plan_rephase_test.go:276: git push … Filename too long |
| TestN124PlanRephaseCompletedReplayRejectsCorruptReceipt | длинный путь | plan_rephase_test.go:774 |
| TestN124PlanRephaseEvidenceBoundsAndExactFullID | длинный путь | plan_rephase_test.go:1128 |
| TestN124PlanRephaseInterruptedJournalRejectsBlindReplay | длинный путь | plan_rephase_test.go:661 |
| TestN124PlanRephaseJournalLinkedAncestorHold | длинный путь | plan_rephase_test.go:957 |
| TestN124PlanRephaseJournalSnapshotLinkedFileHold | длинный путь | plan_rephase_test.go:1064 |
| TestN124PlanRephaseLinkedControlledPlanDirectoryHold | длинный путь | plan_rephase_test.go:939 |
| TestN124PlanRephaseMalformedAndSymlinkEvidenceHold | длинный путь | plan_rephase_test.go:748 |
| TestN124PlanRephasePreparedCrashAfterEveryWriteReconcilesSnapshots | длинный путь | plan_rephase_test.go:839 |
| TestN124PlanRephasePublicCommandHoldsWithoutTrustedAuthorityVerifier | длинный путь | plan_rephase_test.go:263 |
| TestN124PlanRephaseReceiptFailureRestoresFilesAndHoldsEvent | длинный путь | plan_rephase_test.go:798 |
| TestN124PlanRephaseRejectsHeaderDriftDuplicateReleaseAndNonOpenNode | длинный путь | plan_rephase_test.go:435 |
| TestN124PlanRephaseRejectsOwnerDriftBulkShapeAndAuthorityMismatch | длинный путь | plan_rephase_test.go:395 |
| TestN124PlanRephaseRejectsStaleDocumentCASAndForeignRef | длинный путь | plan_rephase_test.go:379 |
| TestN124PlanRephaseRejectsWrongRepositoryMissingAuthorityAndUnicodeMismatch | длинный путь | plan_rephase_test.go:508 |
| TestN124PlanRephaseRequestIDCannotRebindAfterSuccess | длинный путь | plan_rephase_test.go:541 |
| TestN124PlanRephaseSameParentRequiresUniqueNodeSpine | длинный путь | plan_rephase_test.go:1001 |
| TestN124PlanRephaseWrongActorAndStaleAuthorityGenerationHold | длинный путь | plan_rephase_test.go:621 |
| TestN124ScopedNodeRefsAtomicRebindAndIdempotentReadback | длинный путь | plan_rephase_test.go:1152 |
| TestN124ScopedNodeRefsRejectPartialStaleAndLostM3 | длинный путь | plan_rephase_test.go:1217 |
| TestN124SpoofedUserPromptMarkerCannotMintAuthority | длинный путь | plan_rephase_trust_test.go:227 |
| TestN124TrustedGrantCannotRebindRunIDActorGenerationOrMAC | длинный путь | plan_rephase_trust_test.go:116 |
| TestN124TrustedGrantExpiryAndMalformedPromptFailClosed | длинный путь | plan_rephase_trust_test.go:205 |
| TestN124TrustedIssuerMissingUserPromptFailsClosed | длинный путь | plan_rephase_trust_test.go:45 |
| TestN124TrustedIssuerRequiresHostProvenance | длинный путь | plan_rephase_trust_test.go:66 |
| TestN124TrustedUserPromptExactGrantEnablesOneNativeRequestAndReplay | длинный путь | plan_rephase_trust_test.go:81 |
| TestN130RollbackRejectsNestedJunctionWithoutDeletingOwnExternalFixture | окружение: нет junction-фикстуры | update_cache_transaction_windows_test.go:250: Windows unprivileged nested junction fixture is required |
| TestN139BoundReviewRejectsChangedTaskBeforeDispatch | код/фикстура | n139_bound_review_test.go:69: live file differs from HEAD: plan/N-139_… |
| TestN139CodingTaskSelection | длинный путь | operational_run_test.go:136: git add . exit 128 |
| TestN139CoordinatorHandoffWaitAndProtection | код/фикстура | operational_run_test.go:1029: sequence "", want "model,wait,ack,commit,advance,factual,bound-review" |
| TestN139DescriptorTaskSelection | длинный путь | operational_run_test.go:248: git add . exit 128 |
| TestN139ManagedTaskChangedBeforeDispatch | код/фикстура | operational_run_test.go:905: N139_CODING_TASK_UNBOUND: live file differs from HEAD |
| TestOperationalGoalAdvanceRecordedN124Cancellation | длинный путь | operational_goal_advance_test.go:32: checkout … Filename too long |
| TestSemanticReviewerRegistersReceipt | тайминг (флейк) | semantic_test.go:407: semantic receipt did not expose a running PID |

Тайминговые флейки только кандидата А1 в том же прогоне: `TestN123GoJudgeTimeoutSealsOwnedProcess`,
`TestN139FactualSuccessSealsActualExit`; в отдельном прогоне `-count=3` — 3/3 PASS.

Источники: полные логи `base-test.txt` / `cand-test.txt` в scratchpad сессии Dev Work 35
(временный каталог R:, не постоянное хранилище).
