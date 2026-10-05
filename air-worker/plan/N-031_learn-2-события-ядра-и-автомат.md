---
id: "N-031_learn-2-события-ядра-и-автомат"
title: "LEARN-2: события ядра и автоматический review без Claude transcript"
parent: "Self-learning Hermes parity 20261005"
trigger: "ЛПР 05.10.2026: еще раз перепроверь твои предложения, если ок, переходи самостоятельно к кодировке; кодирует эта сессия, судья Codex Astra (gpt-6-astra). Выпуск, установка и live-блокировка без отдельного да запрещены."
owner: "gpt-airworker-learning-20261005"
done_when: "Depends LEARN-1. run_id/principal/session/version, исход, судья или unjudged_reason идут в один журнал; feedback и node close не теряются. Каждый завершённый прогон с новым событием инициирует ограниченный review и RV candidate/no_candidate/error; nil reviewer не равен no_candidate. Повтор/рестарт не теряет review и не применяет правку дважды. GPT finalize имеет штатный transport; отсутствующий Claude transcript не блокирует его. Judge: trigger/restart/duplicate/timeout/host parity contract tests + Codex Astra. Receipt LEARNING_20261005_REVIEW.json."
status: "open"
return_to: "PLAN.md"
created_at: "2026-10-05T03:44:35.1478415Z"
updated_at: "2026-10-05T03:44:35.1478415Z"
receipts:
---

# LEARN-2: события ядра и автоматический review без Claude transcript

- Родитель нити: Self-learning Hermes parity 20261005
- Владелец: gpt-airworker-learning-20261005
- Готово когда: Depends LEARN-1. run_id/principal/session/version, исход, судья или unjudged_reason идут в один журнал; feedback и node close не теряются. Каждый завершённый прогон с новым событием инициирует ограниченный review и RV candidate/no_candidate/error; nil reviewer не равен no_candidate. Повтор/рестарт не теряет review и не применяет правку дважды. GPT finalize имеет штатный transport; отсутствующий Claude transcript не блокирует его. Judge: trigger/restart/duplicate/timeout/host parity contract tests + Codex Astra. Receipt LEARNING_20261005_REVIEW.json.
