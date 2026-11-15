# Самообучение · этапы А и Б · заявки на native-узлы · 15.11.2026

Исполнитель: Dev Work 35. Куратор: Voody (Vega).
Основание: коррекция плана самообучения 15.11.2026 в волте AirWorker_Wiki,
`00_STRATEGY/AIRWORKER_HERMES_SELFLEARNING_PARITY_DEVELOPMENT_PLAN_20261018.md`.

Узлы в этой ветке не заводятся: линия PLAN одна, номера назначаются автоматически.
Решение куратора 15.11.2026: работа идёт под существующими открытыми узлами, а
детальные узлы одной операцией заводит writer канонического дерева при слиянии
(`air-worker plan node new`). `PLAN.md` и `plan/N-*` в этой ветке не меняются.

| Шаг | Родитель | Заголовок | Done_when (кратко) |
|---|---|---|---|
| А1 | N-121 | A1 CLI learning signal: contentless exits counted, not learned | Untyped exit 2, judge exit 1 and help requests create no learning event; typed validate causes still learn; per-command invocation counter with denominator readable by `learn self cli-stats`; 7 days after install show no template-only events |
| А2 | N-110 | A2 reviewer error rate below 10% | Trace which catalogue/root the reviewer sees and what `reviewer_failed` exit 2 is; error share of `reviews` over 7 days below 10% |
| А3 | N-110 | A3 one class, one procedure | Proposals for an existing class patch its target instead of adding a file; CLI itself prints "judge not configured → run unjudged"; merging the 32 live duplicates goes through the LPR gate with a plan and rollback |
| А4 | N-110 | A4 prompt-relevant procedure delivery with a byte limit | Relevance selection (source slice `719b0c8`) shipped; per-turn insertion within the limit and on the task class |
| А5 | N-034 (существующий) | — привязка, новый узел не нужен | Delivery to Hermes Desktop, default profile, via Desktop Gateway (LPR 15.11); `delivered` with readback |
| А6 | N-076 | A6 self-learning owner moved to the installed release | `learn self status` shows no `worktrees` path; history and skills kept; readback before and after, rollback |
| Б1 | N-125 | B1 pilot metric: exit-2 share of operator calls | One number per period from `learn self cli-stats`; two runs on the same data agree; target ≤10% (LPR 15.11), definition open — see note |
| Б2 | N-125 | B2 sealed task set and runner | The improving agent cannot read the set or the check; the run is reproducible |
| Б3 | N-125 | B3 one change, one measurement | Five full steps with numbers before and after, at least one rollback |
| Б4 | N-125 | B4 attempt journal and budget | Journal readable by a native command; the budget limit stops the cycle with a receipt |
| Б5 | N-125 | B5 weekly LPR control number | Share of new violations in classes with an applied rule, included in the A5 summary |

**Замечание к Б1.** В исходниках AirWorker код 2 — общий код отказа: 702 места
`return 2`, из них рядом с разбором аргументов — около 112. Число «доля вызовов с
кодом 2» поэтому измеряет все отказы, а не ошибки синтаксиса. Определение метрики
вынесено куратору и ЛПР; счётчик хранит код выхода и признак help, поэтому любое
из определений считается по тем же данным.
