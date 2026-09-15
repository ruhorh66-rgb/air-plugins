# AirWorker session handoff — 2026-09-15 13:15 +08:00

Канонический исполняемый источник остаётся `PLAN.md`; этот файл только checkpoint состояния сессии.

- Branch: `main`; remote: `origin/main`.
- 0.10.3 выпущен: tag `air-worker--v0.10.3`, release commit `30e43f3`.
- Backlog ASW + AirVera + AirLegal сведён в PLAN: commit `64cdef3`.
- Шаг 81 DONE: Codex Semantic Judge получает prompt через stdin, не argv; commit `3c49e75`.
- Live Windows acceptance шага 81: `codex.cmd`, semantic packet 45 754 chars, terminal PASS.
- Шаг 82 остаётся OPEN/WIP: step-scoped factual verdict + standalone `air-worker semantic`.
- WIP отделяет global factual verdict цели от factual verdict текущего шага.
- Future criteria больше не должны загрязнять semantic review текущего шага.
- `combinedAcceptance`: factual 1 = FAIL, factual 2 = NOT_PROVEN.
- Standalone reviewer routing: Claude→Codex, Codex→Claude, ChatGPT/RDC→Claude.
- Target tests PASS; full `go test ./...` PASS; `go vet ./...` PASS; `git diff --check` PASS.
- Шаг 82 НЕ закрывать до live acceptance; 0.10.4 НЕ выпускался.

## Точка продолжения

1. Smoke product: `R:\-4-\air-worker-semantic82-smoke2`, baseline commit `f209f58`; `current.txt` изменён после baseline.
2. Проверить global judge: ожидается code 1 из-за будущего К2.
3. Запустить dev `air-worker semantic -product ... -step 1 -executor claude`; semantic packet должен иметь step factual code 0.
4. Проверить `.woody/semantic-verdict.json` и history: read-only reviewer, step scope, terminal verdict.
5. Только после live PASS закрыть шаг 82 в `PLAN.md`, затем gate 81а/релиз 0.10.4 по решению ЛПР.
6. Следующий слой после 0.10.4: 79 → 71 → 72 → 73 → 74 → 75 (operational kernel 0.10.5).

Старый `R:\-4-\air-worker-semantic82-smoke` не форсировать: его `.git` дал Access denied при cleanup; использовать `smoke2`.
