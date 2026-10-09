# AirWorker N-101 — фактическое самообучение установленного 0.11.8

Дата: 09.10.2026, SRVLM01. Приоритет ЛПР: проверять самообучение **установленной** версии на дефектах собственной сессии. Статус: **PASS для product-scoped shared learning; автономное self-learning самого AirWorker — NOT_PROVEN**.

## Проверенный контракт

- Installed CLI: AirWorker **0.11.8**, revision `cbfa266eefe8d5d1ea67db078bc416f4a928147a`, selfcheck PASS. Claude/Codex plugin caches 0.11.8 соответствуют бинарнику; реальный 0.11.9 ещё не установлен.
- `air-worker learn self status -json` возвращает rc=2: `unknown learn action "self"`. `air-worker learn status -product <AirWorker root> -json` возвращает rc=0. Shared module существует, но постоянный self-owner в 0.11.8 отсутствует.
- В начале опыта: 66 событий, 7 предложений, 7 применённых процедур. В конце измерения: 71 событие, 9 предложений, 10 ledger-транзакций, из них одна транзакция rollback.
- Реальная ошибка отправлена через **штатный** `air-worker feedback add` с run_id `AW-N101-SELF0118-MISSING-OWNER-20261009-1`: event `EV-19bb67345c8773671359b538`, auto-review `RV-88d2909a0596fd3e2ccecd3d`, candidate `LP-EV-19bb67345c8773671359b538`, первый auto-apply `TX-4de9c4dc4b91ecdd7e3904de`. Reviewer — release-owned `gpt-5.6-luna medium`. Доставку события запустил оператор через native CLI; она **не** была автономным host Stop.
- **Новый реальный дефект N-102:** safe-review целиком заменил полезный прежний навык `skills/learned/feedback-error-a0309df4.md` (SHA `e0b274857a6e2a1c3f9a0e5febb2b25acbd083793ce325eccaed02442aa9aec9`) несвязанной процедурой. Потери предотвращены штатным `learn rollback` — исходный SHA восстановлен. Новая процедура отдельно применена через штатный `learn propose` под `skills/learned/self-owner-verification-6ff81af9.md` с SHA `6ff81af995e5df9256830e181db2e4004859f16b5e5d7cc15e9c7d7e55fa880d`. **Второе применение — ручное ретаргетирование ранее сгенерированного reviewer текста через ядро**, не второй автоматический reviewer.
- **Свежий процесс:** `air-worker learn load` с отдельным run_id получил `status=loaded`, exact SHA, event `skill_loaded`, `used=false`. `procedure_used` в общем runtime: 0. Нельзя утверждать фактическое использование или эффект навыка. Узлы N-073/N-093 остаются OPEN.
- **N-096 подтверждён эксплуатацией:** `feedback list` содержит событие, но не появилось нового immutable `FB-*.json` и non-executable PLAN candidate. **N-095 подтверждён:** `learn --help` rc=2 вместо справки.
- **N-103:** независимый Astra high по ещё не выпущенному WIP-коммиту `f1cde6a6` дал `CHANGES_REQUIRED` (5×P1, 3×P2). Эти дефекты нового кода **не приписываются установленной 0.11.8**.
- Скрипт запуска обратной связи после успешного завершения нативного reviewer завис и тратил CPU; его native exit-code **UNKNOWN**. Проверено, что это собственный PowerShell-wrapper без активного дочернего AirWorker; остановлен только он, не службы. Корневая причина зависания wrapper не установлена.

## Канонические свидетельства

- Машинная квитанция: `docs/receipts/AIRWORKER_N101_INSTALLED_0118_REAL_SELF_LEARNING_20261009.json`.
- Baseline и отдельные receipt rollback / retarget / fresh load / cleanup: `R:\-4-\air-worker\work\0.11.9\n101\`.
- Исходный журнал, review и ledger: `R:\-4-\air-worker\self-learning\`. **Не редактировать эти файлы вручную**.
- План и узлы: `PLAN.md`, N-101 (аудит), N-102 (P0 сохранение прежних навыков), N-103 (5 P1+3 P2), N-095, N-096, N-093, N-092, N-071.

## Следующая измеримая веха

В первую очередь — не допускать потери прошлых процедур при auto-review (N-102) и ложных `procedure_used` (N-103). Далее N-096/N-095, фактический host E2E с верифицированным `used/outcome/effect` (N-093), общий судья N-092 и релиз N-071. Установка 0.11.8 не менялась. Release/tag/marketplace/install требуют отдельных точных разрешений ЛПР.
