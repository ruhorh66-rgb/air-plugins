# AirWorker N-120 — WIP handoff, 11.10.2026

**Статус:** STOPPED / WIP CHECKPOINT / **P1 REPRODUCED, NOT FIXED**. Не релиз, не PASS N-120. ЛПР поручил сохранить состояние для нового окна. Числа времени для ЛПР — SGT.

## Где продолжить

- Node: **SRVLM01**, доступ **только через AIR Commander** с явным \`node: "SRVLM01"\`.
- Рабочее дерево: \`R:\-4-\air-worker\worktrees\airworker-0120-native-judge-20261011\`; продукт: \`air-worker\`.
- GitHub: \`https://github.com/ruhorh66-rgb/air-plugins\`, ветка \`dev/airworker-0.11.9-selflearn-judge-preflight-20261011\`. Исходный принятый код до WIP: \`3ecea9077b425e14cdf456fbfc2e364d56758911\`.
- **Единственная авторитетная линия 0.11.9:** \`R:\-4-\air-worker\worktrees\airworker-0119-selflearn-isolated-20261011\`, ветка \`dev/airworker-0.11.9-selflearning-only-20261011\`, HEAD при проверке \`d5ee3c42a72e8f9f1db16649a97ad786b136e0fb\`; она **не переключалась и не менялась** в этой остановке.
- План: текущий \`air-worker/PLAN.md\` и \`air-worker/plan/N-120_p0-auto-learn-real-native-judge-configuration-fa.md\`; parent \`N-110_p0-auto-capture-persist-every-real-airworker-ses\`. Узлы **N-120, N-110, N-093 открыты**; N-118/N-119 ранее закрыты по отдельной проверенной машинной приёмке. Не закрывать N-120 по одному RED.
- Установлен AirWorker **0.11.8**; выпуск/установка 0.11.9, перезагрузка, UAC, чужие службы — без нового явного «да» ЛПР запрещены.

## Что уже подтверждено

1. Источниковый коммит \`3ecea90\`: нативный \`judge\` с ошибкой отсутствующей конфигурации возвращает код 2, HMAC-квитанция фиксирует типизированную причину, передаёт единственное событие модулю, вызывает тестовый reviewer, безопасный Markdown применяется без ложного \`procedure_used\`.
2. **Полный Go QA только на чистом \`3ecea90\`:** \`go test ./...\` exit 0, \`go vet ./...\` exit 0, build exit 0. Артефакт \`R:\-4-\air-worker\work\0.11.9\n120-judge-3ecea90\QA_FULL.json\`; бинарник SHA-256 \`6c2641961f1abb38646d226aa0e3302d9241fc34624d9b555dceb5777cca2d35\`.
3. Независимый **Codex GPT-6 Sol Low** по \`3ecea90\`: **CHANGES_REQUIRED, 0 P0 / 1 P1 / 0 P2**, \`SOL_LOW_VERDICT.json\` SHA-256 \`3accc0c78dab5ae555e5b7779346363dd75a7315e59a3714fa72b35230755fdf\`. Это НЕ PASS.
4. **P1:** \`air-worker/cmd/self_cli_trusted_cause.go:130\` — функция \`ownCLITrustedJudgeFailureCause\` проверяет только конечную папку \`-product\`. Родительская папка может быть Windows junction/reparse и перенаправить источник в чужое дерево; тогда будет ошибочно доверен факт \`judge-run-config-missing\`.
5. Единственное незакоммиченное изменение перед остановкой было в \`air-worker/cmd/self_judge_preflight_test.go\`: добавлен \`TestN120JudgeRejectsAncestorJunctionToForeignProduct\` с отдельным Windows \`mklink /J\` fixture. Этот тест **RED, exit 1**: классификатор вернул \`judge-run-config-missing\` через alias. Исходная квитанция \`R:\-4-\air-worker\work\0.11.9\n120-judge-3ecea90\P1_PARENT_JUNCTION_TDD_RED.txt\`, SHA-256 \`a0b27485a638185f0be3b3dea80d20a50f8445128e8ed94db53bf558e7bcff3a\`.
6. \`git diff --check\` PASS; на момент контроля **нет активного теста Go N-120**. Важно: полный QA и исходный независимый вердикт относятся к старому коммиту \`3ecea90\`, а НЕ к сохранённому после него WIP-тесту.

## Точное продолжение в новом окне

1. Войти через AIR Commander; сначала \`list_nodes\`, сверить \`git log -1\`, \`git status --porcelain\`, GitHub remote SHA, \`air-worker plan spine/node list\`, последние QA и RED receipts. **Не повторять** \`mklink\`/Go RED вслепую и не выполнять \`git reset/clean\`.
2. Исправить P1 в новой текущей WIP-ветке: **проверить каждый компонент абсолютного пути продукта от корня тома до \`-product\`** на symlink, Windows junction/reparse, ошибки чтения и подмену идентичности; fail-closed. Использовать штатные примитивы \`cmd/feedback_data_file_guard.go\` / \`feedback_data_file_identity_windows.go\` или аналог без дублирующей логики. Сохранить приватность оригинальной signed CLI capture. Изменять \`cmd/self_cli_trusted_cause.go\` только после RED.
3. Проверить \`TestN120JudgeRejectsAncestorJunctionToForeignProduct\` → GREEN и дополнительные тесты на обычный корень, missing \`run-config.json\`, PLAN, malformed config, \`-config\` и посторонние argv. Затем scoped \`TestN120|TestN117|TestN110|TestN118\`.
4. Полная регрессия / \`go vet\` / build на **новом clean commit SHA**; отдельный read-only Codex \`gpt-6-sol\` \`model_reasoning_effort=low\`, **0 P0/0 P1**. Только после PASS — новый изолированный бинарный smoke с **настоящим** reviewer (тестовый reviewer из \`TestN120\` не является таким доказательством).
5. Создать JSON-квитанцыи и source handoff по факту; \`air-worker plan node close N-120...\` **только при всех done_when/PASS**, затем отдельный commit+push и проверенный fast-forward авторитетной 0.11.9 ветки без force. Родитель N-110 и N-093 пока открыты. Не трогать вручную \`plan/N-*\` или \`learn/events.jsonl\`; все изменения — через ядро.

**Проверка после остановки:** WIP намеренно оставлен с воспроизведённой ошибкой; код P1 не исправлен, выпуск/установка/перезапуск не производились.
