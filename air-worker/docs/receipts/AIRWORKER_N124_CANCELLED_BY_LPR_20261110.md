# N-124 — отмена задания решением ЛПР

Дата решения: 10.11.2026.
Решение: ЛПР прямо распорядился закрыть и убрать N-124 из активного плана работ; выполнение прежнего технического done_when не требуется.
Точный узел: `N-124_p1-detect-cross-branch-node-id-collision-and-sta`, родитель `N-110_p0-auto-capture-persist-every-real-airworker-ses`.

Disposition: **CANCELLED_BY_LPR / CLOSED_WITHOUT_EXECUTION / NOT_TECHNICAL_PASS**.

Это управленческая отмена пункта плана, а не подтверждение полноты проверки cross-branch ID, native CAS, rephase или release-to-plan binding.
Оставить исходные исторические receipts, Git commit и прежние планы для аудита, не повторять задачу и не требовать N-124 PASS в действующих зависимостях.
Технические инварианты, не входившие в отменяемый пункт (подпись Ed25519, SHA/manifest, пакетная проверка, независимый reviewer, rollback), остаются действующими.
При выпуске AirVera 0.2.11 не предъявлять отменённый N-124 как условие; вместо него использовать уже имеющиеся предметные release-signing/QA/rollback проверки. Текущий код `tools/plan_release_binding.py` отдельно требует устранения устаревшего hold — эта квитанция сама по себе НЕ означает, что code-gate уже изменён.
Чужие продукты, installed AirWorker, AIR OS, Telegram/Hermes и рабочий релиз AirVera не меняются этим решением.
