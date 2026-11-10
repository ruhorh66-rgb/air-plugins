# AirWorker N-126 — handoff 11.11.2026 SGT

Статус: SOURCE_ONLY. N-126 OPEN. AirHR N-057/N-058 OPEN. Никакой установленный релиз не менялся.

## Контур и авторитетные материалы

- GitHub repo: https://github.com/ruhorh66-rgb/air-plugins
- Source worktree: D:/Devbox/AirWorker/worktrees/airworker-01111-rc2-20261031
- Branch: dev/airworker-native-reviewer-readonly-20261110
- Исходный HEAD до изменений: 0befb40e246b091916c7f321db2b3c56b36c3ade
- Актуальный SHA опубликованного source checkpoint сверять по Git HEAD/remote; не использовать base SHA вместо него.
- N-126: plan/N-126_p1-enforce-native-claude-reviewer-read-only-in-a.md, создан только через air-worker plan node new.
- Патч: air-worker/skills/run-worker-task/scripts/claude_judge_run.py
- Тесты: air-worker/skills/run-worker-task/tests/test_claude_judge_readonly.py
- Квитанция: docs/receipts/AIRWORKER_REVIEWER_READONLY_20261110.json
- Native AirWorker LEARN: LR-20261110T233634Z-9184f30c
- Обязательное правило: F:/-5-/011_Plugins/AirWorker_Wiki/00_REGISTRY/RULES/RULE_CLAUDE_TOKEN_LAUNCH.md

## Причина, решение, границы результата

Независимый Codex CLI 0.167.0, модель gpt-6.1-terra в read-only, обнаружил P1: установленный AirWorker0.11.11-rc.2 helper доставляет существующий пользовательский токен через Headroom, но запускает Claude с аргументами -p --model --output-format json без ограничения write-capable tools.
Профиль Claude не доказывает read-only: hooks охватывают Bash/PowerShell, но не Edit/Write. Интерактивный OAuth expired сам по себе не доказывает истечение долговременного токена.

Source-only patch в текущей ветке добавляет фиксированные Claude flags: --restricted, --tools Read,Grep,Glob, --strict-mcp-config, --disallowedTools Write,Edit,Bash,PowerShell,Task,mcp__*, --permission-mode plan, --permission-prompts none, --no-session-persistence. Контракт двух аргументов, stdin prompt и доставка существующего токена сохранены.

Синтетический pytest: 8/8 PASS с mock subprocess: argv, stderr/error fail closed, UTF-8 stdin, запрет небезопасных инструментов в списке и отсутствие вывода тестового токена. Это НЕ отрицательный реальный тест Claude. Independent Codex postpatch source review и managed settings/MCP/Task/Write/Edit runtime tests НЕ подтверждены. Вердикта Anthropic нет. N-126 не закрывать до доказательства результата.

## Деревья и выполненная безопасная очистка

На SRVLM01 зарегистрировано 423 AirWorker Git worktree, 374 coding-workspaces; одна запись относится к отсутствующему пути.
После штатного git worktree prune удалены только две записи Git о несуществующих папках: AirWorker 423 -> 421, никаких физических worktree не удалено. У AirHR dry-run выявил четыре устаревшие записи; из-за ранее подтверждённых ACL Permission denied не выполнялись повышение прав и принудительная очистка. У AirHR — 17 worktree, 17 существуют, 7 имеют tracked changes; шесть старых detached QA деревьев имеют сотни изменений.
Нельзя массово удалять или force prune чужие деревья. Сохранять историю и владение; к целевому минимуму product/Wiki переходить после индивидуальной проверки владельцев, SHA и dirty state.
Удалены только шесть собственных синтетических фикстур из air-worker/.test_tmp/claude-readonly-s0-20261110 и пустой каталог, после проверки отсутствия тестовых процессов. Другие worktree не изменены.

## Порядок следующей сессии

1. Прочитать PLAN.md, N-126, исходный commit SHA, эту квитанцию и native LEARN. Проверить Git SHA clean/remote.
2. Провести независимое postpatch Codex-ревью с file:line и результатами, исправить замечания при наличии, повторить native tests.
3. При отдельно разрешённом безопасном изолированном QA проверить реальный Claude2.1.319: Read/Grep/Glob разрешены; Write/Edit/Bash/PowerShell/Task/MCP запрещены, без побочных файловых изменений. Учесть ограничения --restricted, user/project/managed settings и отсутствие побочного ослабления Headroom.
4. Установленный AirWorker0.11.11-rc.2 и его helper SHA9ed6329 не менять без отдельного точного да ЛПР на релиз и установку. Использовать GitHub release, регрессию и откат.
5. После подтверждённого read-only установленного helper провести независимое Anthropic claude-sonnet-5-5 review точного AirHR frozen SHA a780826a629913a1177ae051239b750cb55d8969, 20 файлов с реальными file:line evidence и вердиктом. Никакого PASS без чтения кода.
6. В AirHR остаётся открыта Telegram→Core candidate SQL бизнес-веха. Пара Core0.10.10+Hermesrc31 опубликована как REVIEW_PENDING/NOT_INSTALLABLE. Установлены Core0.10.9, Hermesrc29 и SQLite7; Gateway не перезапускался. Утверждение принадлежит ЛПР.

Никаких новых ключей, изменений профиля Claude, остановки служб, UAC/reboot, установки релиза или изменения HR SQL в этой сессии.
