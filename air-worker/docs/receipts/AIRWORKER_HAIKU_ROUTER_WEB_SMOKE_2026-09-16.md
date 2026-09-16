# AirWorker Haiku→Router web-provider smoke — 2026-09-16

## Решение ЛПР
- `script` остаётся детерминированной ступенью.
- `haiku:medium` и `haiku:max` исполняются через `runner kind=router` → OpenCode/AirLLMRouter.
- `sonnet` и `opus` через Router пока не идут.
- Отдельная активная ступень `router` не нужна; это transport/policy для Haiku.

## Реальный тест
Задача: production-фрагмент шага 86 — `air-worker tool -which router` должен быть dry-run topology diagnostics, не искать `router.exe`, печатать `runner=router`, `shell=opencode`, `AirLLMRouter` и возвращать 0.

Внешний acceptance: `TestCriterion70RouterToolDiagnosticsSmoke`.

## Qwen Web
- Вызов прошёл через AirLLMRouter.
- Router status: `200`, provider/effective_model: `qwen-web`, finish=`stop`, session_state=`idle_closed`.
- Модель предложила ограниченную production-правку `cmd/tool.go`.
- Правка применена только в isolated worktree `aw-haiku-real-nex`.
- Внешний test: PASS.
- `git diff --check` для production diff: PASS.
## Codex judge
- Step factual: PASS (`К1`).
- Semantic: `NOT_PROVEN`, step=`partial`, drift=`minor`.
- Причина: one-shot Router coding не создал AirWorker executor job/route receipt; Codex не оспорил сам production diff.

## DeepSeek Web
- Профиль `E:\-4-\air-llm-router\web-profiles\deepseek-web` существует и обновляется.
- Попытки через AirLLMRouter: `RATE_LIMIT` и `TRANSPORT`; реальный coding-result не получен.
- Повторные вызовы не продолжались, чтобы не продлевать provider spacing/rate limit.

## OpenRouter free
- В ходе smoke исчерпан дневной free-tier лимит: 50/50, remaining=0, HTTP 429.

## Дополнительный regression-факт
Полный Go regression isolated Qwen worktree упал не на `cmd/tool.go`, а на старом контракте `TestRouterFirstReleaseProfileKeepsCodexJudge`, который требует лестницу `script -> router`. После решения ЛПР этот тест должен быть синхронизирован с `script -> haiku`, где `haiku` использует runner `router`.

## Вывод
Есть фактическое подтверждение, что Qwen Web через AirLLMRouter способен решать небольшой реальный coding-блок уровня Haiku. Полный agentic acceptance через AirWorker/OpenCode пока не доказан; его блокируют transport/receipt integration и provider limits, а не внешний Go acceptance самого кода.
