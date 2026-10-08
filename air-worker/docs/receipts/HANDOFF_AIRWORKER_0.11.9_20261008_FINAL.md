# AirWorker 0.11.9 — handoff / завершение сессии 08.10.2026

**Статус: SAVED / DEVELOPMENT CHECKPOINT; N-097 OPEN для возобновления. Релиз 0.11.9 НЕ опубликован и НЕ установлен.**

Это новое, актуальное продолжение прежнего F:\-5-\011_Plugins\AirWorker_Wiki\03_REVIEW\HANDOFF_AIRWORKER_0.11.9_20261008.md (старый handoff N-078 на SHA 425c83f; он исторический). Источник истины для текущего HEAD — GitHub remote ветки и финальная машинная квитанция R:\-4-\air-worker\work\0.11.9\HANDOFF_AIRWORKER_CURRENT_20261008_FINAL.json. HEAD старого checkpoint 472db2d не является вечным требованием: этот handoff сам добавляет следующий commit.

## 1. Авторитетная среда и инструкции

- Сервер: **SRVLM01**, через AIR Commander с **явным node=SRVLM01**. AIR-ENV-002 на момент проверки не в сети.
- Product worktree: F:\-7-\_worktrees\airworker-curator-skill-lifecycle-20261006
- Product/PLAN: F:\-7-\_worktrees\airworker-curator-skill-lifecycle-20261006\air-worker
- Branch: dev/airworker-curator-skill-lifecycle-20261006
- Канонический GitHub remote называется **github**: https://github.com/ruhorh66-rgb/air-plugins.git; локальный remote origin указывает на другой worktree, его НЕ использовать для внешнего push.
- Branch без configured upstream: сравнивать явно local Git HEAD с GitHub refs/heads/dev/airworker-curator-skill-lifecycle-20261006.
- Handoff node: N-097_handoff-0-11-9-verified-github-checkpoint-and-sa. Создан ядром и оставлен OPEN для factual restart.
- Следующей сессии сначала прочесть PRODUCT_INSTRUCTION.md, PLAN.md, plan/N-097*, N-071, N-073, N-076, N-092, N-093, N-094, N-095, N-096, этот документ и JSON рядом с ним.
- История содержит 46dddab и 4ee4fb4 как старые checkpoint, ранее закрытый N-080, scoped acceptance commit bc33045 и отдельные позднейшие plan/learned commits.

## 2. Установленный продукт и module authority

- Установленный AirWorker: **0.11.8**, revision cbfa266eefe8d5d1ea67db078bc416f4a928147a, Claude/Codex plugin cache тоже 0.11.8; native selfcheck PASS. Не устанавливать 0.11.9 без отдельного точного «да» ЛПР.
- Кодовый proof checkpoint 0.11.9: bc330453290d6e2eebbbd7aec74f1f006a35c967; этот коммит получил full Go tests/vet PASS, fresh Windows checkout core.autocrlf=true PASS и независимый GPT-6 Astra high PASS (0 P0/P1).
- Квитанции: R:\-4-\air-worker\work\0.11.9\n090\consumer-bc33045\FULL_bc33045.json; R:\-4-\air-worker\work\0.11.9\n090\fresh-bc33045-proof\FRESH_bc33045.json; R:\-4-\air-worker\work\0.11.9\n090\astra-bc33045\verdict.json.
- N-078 и зависимые N-080–N-091 закрыты **через ядро AirWorker** со ссылкой на N078_ACCEPTANCE_bc33045.json; immutable acceptance копия: docs/receipts/AIRWORKER_N078_ACCEPTANCE_bc33045_20261008.json. Это **scoped PASS**, не release-level PASS.
- Общий модуль: F:\-8-\_worktrees\air-modules-n082-20261008, HEAD 8fcd458a783e2dbffb1d590ba838e44bedf6dda8, branch dev/airworker-n082-lock-20261008; GitHub remote origin у этого отдельного репозитория. Go pin: github.com/ruhorh66-rgb/air-modules v0.0.0-20261008142557-8fcd458a783e. Full module test/vet PASS: R:\-4-\air-worker\work\0.11.9\n090\module-8fcd458\MODULE_FULL_8fcd458.json.
- Отдельный tag/publication shared learning 0.1.0, обновление AirWorker с commit pin на released version, публикация AirWorker, marketplace promotion и установка live — будущие раздельные гейты ЛПР.
- Позднейшие текущие коммиты — изменения PLAN и отслеживаемые learned skills; **не** новый независимый full Go/Astra PASS на финальном handoff HEAD. После будущих code edits заново тестировать точный immutable commit.

## 3. Сохранённое auto-learning и clean tree

На исходном продукте четыре ранее untracked навыка сохранены **без правки байтов** отдельным коммитом 472db2dc04fc3955422fb14efd0ccdc3ad536be3. Для каждого точный SHA256 файла == last status=applied/post_sha256 штатного R:\-4-\air-worker\self-learning\ledger.jsonl; все LF, Git attributes text eol=lf:

- skills/learned/cross-product-reboot-handoff-ownership-e-348f5602.md — ed1c2ba8b27a421bd872fa04bf0be7233814b4b40640a8958a351abb4eac5f37
- skills/learned/feedback-error-a0309df4.md — e0b274857a6e2a1c3f9a0e5febb2b25acbd083793ce325eccaed02442aa9aec9
- skills/learned/public-approval-surface-53771d27.md — f6edc33c7bb9b48d74dedec2fef243e37a05dac38a4073a7053fdee0fbffbc2a
- skills/learned/release-host-parity-6c083dd3.md — 9b1bb1a58a882a0d777ac3c1f346c8168abaea5d59825eccd369f8cebbf8e3c6

Квитанция provenance: docs/receipts/AIRWORKER_LEARNED_APPLIED_SNAPSHOT_20261008.json. Это учебные текстовые процедуры, НЕ инструкции к самовольному reboot/stop/installation и НЕ LPR-гранты. LEARN-журналы/selector НЕ изменялись вручную.

Сохранить как есть:
- R:\-4-\air-worker\self-learning
- R:\-4-\air-worker\work\0.11.9\self-hook-state-candidate\air-worker-self-learning.json
- ignored product-local learning-module.json и .air-learning-owner.json
- Изолированный E2E каталог R:\-4-\air-worker\work\0.11.9\n073\real-cycle-bc33045-20261008-01
- Все receipts и review логи на R:. Не делать git clean/reset/stash, прямые правки plan/N-*, LEARN ledger или чужих worktree.

## 4. Что реально подтверждено, что ещё открыто

**N-073/N-076/N-093 (P0):** В изолированном процессе кандидат AirWorker через native learn self enable перенёс 3 historical APPLIED процедуры. Реальный tool -which headroom вернул код 2/health timeout; Stop записал run_completed за 56 мс; @self production Codex reviewer сам создал и безопасно применил skills/learned/completed-turn-d3508106.md (SHA df049e85e7b093763a81c21b30dceb5a9379ad87c6ac74ef45144ecab6f05ba2), pending=0. **Новая независимая сессия загрузила этот exact SHA**. Повторный реальный отказ не породил ложный procedure_used: это корректно, но **позитивное used/outcome/effect для вновь созданной процедуры не доказано**. Причина: handlePostToolUseSelfLearning пока специализируется на execution-unknown-no-blind-retry. Квитанция: n073 real-cycle ...\n073-real-use-probe.json. Следующий измеримый технический шаг — **N-093** (обобщённое, коррелированное, подтверждённое использование/effect, не выводить PASS из простого loaded).

**N-094/N-095/N-096 (P1):** Эксплуатация GPT-5.6 Sol выявила, что learn --help/learn status/paths без -product уходят в устаревший unknown action, а shared-mode feedback инициирует обучение, но пропускает canonical immutable .air-worker/feedback/FB-*.json и неисполняемый feedback candidate в PLAN. Это подтверждено исходным кодом; N-094 создан и опубликован, N-095 (CLI контракт) и N-096 (двойной feedback intake) созданы через ядро. Сейчас **ещё не исправлены**. Квитанция: docs/receipts/AIRWORKER_N094_EXPLOITATION_INTAKE_20261008.json. N-093 не дублировать.

**N-092 (релизный блокер):** Global product judge на том же code checkpoint завершился rc=1. Check-ladder-reachable не проходит Headroom transport: native bin/air-worker.exe tool -which headroom вернул rc=2, health timeout 127.0.0.1:8787; Ponytail работает. Check-goal-drift: история .woody/goal-drift.jsonl отсутствовала, хотя .goal-verdict.json появился после первого judge. Не ослаблять судью, не выдумывать PASS. machine evidence docs/receipts/AIRWORKER_N092_GLOBAL_JUDGE_FAIL_20261008.json. Ни одну чужую службу не останавливать без отдельного «да» ЛПР.

**N-071 (релиз 0.11.9):** OPEN. Помимо P0/P1: проверить open N-072 и старые N-028–037 против actual receipts, module 0.1.0 отдельным гейтом, обновить PRODUCT_INSTRUCTION/help/changelog, пересобрать версионированный кандидат, точно проверить package SHA, rollback к live 0.11.8, full regression/vet, native selfcheck/dual owner E2E и независимый Astra high. Tag/GitHub Release/marketplace/live install возможны только после отдельного явного «да» ЛПР.

**N-097:** OPEN; закрыть только после фактического входа следующей сессии, проверки clean local=GitHub HEAD и сохранённых receipts через core plan node close.

## 5. Первые действия следующей сессии

1. Через AIR Commander с node=SRVLM01 проверить связь, процессы, Git HEAD/dirty и PLAN spine; не повторять уже успешные мутации.
2. Выполнить git -C <worktree> ls-remote github refs/heads/dev/airworker-curator-skill-lifecycle-20261006 и сопоставить с git rev-parse HEAD; на финальной квитанции R: записан SHA после push.
3. Сверить четыре tracked LF procedures и ledger SHA read-only, оставить ignored и machine-local state.
4. Прочитать N-097/N-093/N-095/N-096/N-092; начать N-093, затем N-095 и N-096, затем N-092 и выпускные зависимости N-071. Избежать параллельного writer в одном worktree.
5. Любую правку через PLAN+native AirWorker; judge/learn/receipt перед закрытием узла. New code release требуются полный regression и независимый Astra high на новом immutable SHA.
6. Не предпринимать reboot, shutdown, UAC, foreign-service stop или live release installation без конкретного отдельного решения ЛПР с проверкой и откатом.

## 6. Handoff artifact integrity

Machine-readable companion: docs/receipts/HANDOFF_AIRWORKER_0.11.9_20261008_FINAL.json.
Authoritative post-push snapshot: R:\-4-\air-worker\work\0.11.9\HANDOFF_AIRWORKER_CURRENT_20261008_FINAL.json.

Настоящий документ не утверждает, что N-093, N-094/N-095/N-096, N-092 или N-071 закрыты. Статус «чистое дерево» требует фактической проверки после последнего push.