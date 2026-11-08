# AirWorker · Dev Work 31 → следующая GPT-сессия · 08.11.2026

## Что именно передаём

Сессия завершает **сохранение и уборку**, а не приёмку P0. Актуальный исходный код на ветке `dev/airworker-native-orch-pilot-20261104`; source baseline перед завершением: `6ed303e143bffa24cd0354489a9f2eb11ad74ef4`. После публикации этого handoff и native learning journal актуальный SHA **читать из Git**, не считать базовый SHA финальным. Wiki: `F:\-5-\011_Plugins\AirWorker_Wiki`, ветка `dev/airworker-curator-0.10.14`, до завершения `c37b4d12a979219e6dd4e1440d5de7cdc4b22752`.

**Штатная метрика:** P0 **4/8 PASS**, G01–G04 приняты, G05/G06/G07/G08 **OPEN**. N123/N126/N136/N139 **0/4 CLOSED**; `operational-goal.json` указывает на текущую Git-ветку без изменения `plan/release-slices.json` SHA `811a37794842d8ed575f478d0a84665579daee5f5afe94e5e4ad6e9e7627ab9a`. Установлена `0.11.11-rc.2`, исходный кандидат `0.11.11-rc.5` лишь `SOURCE_ONLY/HOLD`; install/tag/release **НЕ** выполнялись.

## Что принято в исходниках и адресно проверено — не повторять

1. **Штатная оркестрация**: только ChatGPT-координатор + ровно два реальных Codex-кодера по маленьким `file/instructions/done_when` в разных Git-worktree, независимые native terminal receipts, patch SHA, `git apply --check`, затем адресная QA. История в `PLAN.md` и `docs/tasks/DW31_G05_*.json`, `docs/receipts/DEVWORK31_*.json`. Новых лидеров/мостов нет.
2. **N123 durable jobs**: source/args/TASK SHA identity, immutable receipt, реальные PID/exit/output SHA, read-only recovery без дублей, bounded Go timeout с owned process tree, отказ от чужих путей и ложного `DONE`.
3. **Security G05 по ревью на исторических SHA**: исправлены `.woody/jobs` junction escape, unsealed successful output >32 MiB, deterministic hardlink/TOCTOU в core. Go 1.26 `os.Root`, `O_EXCL`, `Root.Rename` дают bounded rooted receipt/output. Два независимых судьи на `15b457e` обнаружили ещё P1 в `runner_openai.go` и `runner_router.go` — исправлено в baseline `6ed303e` через общие `ensureOwnedJobsDir` / `writeOwnedJobOutput`; router теперь не игнорирует ошибку вывода. Последняя адресная QA `TestN123OwnedRunnerOutputExclusiveLinkAndSeal`, raced hardlink, `TestOpenAIAPIRunner` (локальный httptest), router boundary **RC0**, `go vet RC0`; log SHA `5b492460c372c43855515fe516abb9066881f908f5893536cb8d21b46f9c6991`. Квитанция `docs/receipts/DEVWORK31_G05_SIBLING_ROOTED_OUTPUT_20261108.json`.
4. **NTFS atomic temp names**: длинное имя временной квитанции (>255 в компоненте) ранее блокировало Codex до запуска. Исправлено: краткое hash-based имя внутри того же каталога с прежней atomic/durable семантикой; адресные tests RC0. Не считать такую локальную ошибку G06 provider failure.
5. **N139**: source-bound factual Go receipt, bounded deadline, остановка собственного parent/child; адресные тесты RC0. Общий package QA/финальное ревью остаются отдельной приёмкой.

## Уборка деревьев — подтверждённое состояние

До cleanup: **36** зарегистрированных собственных detached Codex-worktree этой ветки; **4 CLEAN, 32 DIRTY**, у каждого DIRTY ровно один tracked Go-файл, untracked/staged WIP не было. После **побайтового резервирования 32 оригинальных Go-файлов и 32 native patch-файлов, двойной проверки SHA** выполнен только `git restore --worktree --source=HEAD` соответствующего конкретного файла в каждом дереве. Проверено **36/36 CLEAN, 0 пропусков**. Все рабочие каталоги, detached refs, квитанции, handoff и Git-ветки сохранены. **Не выполнялись** `git worktree remove/prune`, удаление веток, чужих worktree или команд на чужих продуктах. Старые prunable foreign записи оставлены.

Архив и скрипт обратимого восстановления: `D:\Devbox\AirWorker\evidence\DEVWORK31_SESSION_CLOSE_20261108`; machine manifest `docs/receipts/DEVWORK31_WORKTREE_CLEANUP_20261108.json` SHA `186d4cb6b5a0e43c5651df00ff630bb0b4410c64c7439e24ac0dae699a009093`. **Не очищать повторно и не терять архив**, если результаты уже опубликованы.

## Что НЕ принято и что делать следующей сессии

- **G05 OPEN:** на последнем baseline `6ed303e` есть targeted PASS, но **нет свежего независимого финального Anthropic + Codex Sol low PASS** на этом SHA, одного полного нативного Go QA с корректной квитанцией, полного отрицательного N124 file-symlink (под текущим Windows токеном `SKIP` из-за отсутствия `SeCreateSymbolicLinkPrivilege`; junction test PASS не заменяет symlink) и reversible rollback. Исторический full `go -C cmd test ./... -timeout=10m -p2` на старом SHA `8f51c81` дал RC1 и package timeout 600s: N124/node.before был показан `(0s)`, он НЕ доказан зависшим на 10m. 836 cmd + 6 tray top-level тестов инвентаризованы; попытка провести 11-part QA через частичный `judge -config` законно отклонена native PLAN guard RC2 **до исполнения тестов**. Не обходить guard.
- **G06 OPEN:** реальный causal provider stall/failure и next-tier escalation по тому же TASK/SHA не доказаны. Синтетика и локальные ошибки — не PASS.
- **G07 OPEN:** пользователь действительно сообщал об обрывах. Восстановлен ранее завершённый read-only Codex `turn.completed` из журнала, без нового запуска; однако требуемые **две разные ordinary GPT-сессии**, единый нативный `job_id`, переход `RUNNING/EXECUTION_UNKNOWN` и zero duplicate не доказаны. Записано также в `.woody/qa/devwork31-final-g05/P0_G07_USER_REPORTED_CHAT_BREAK_20261108_2159SGT.json` SHA `a7a35aced0b395386ec95aa66009d59d33d86cbfa2fd3af0f8a65c641f490ae5`.
- **G08 OPEN:** только после собственных `done_when` и независимой приёмки использовать штатный `plan node close`, не редактировать статусы вручную.
- **Самообучение:** новые события этой сессии должны быть записаны **только** через `air-worker learn event` и прочитаны из `air-worker/learn/events.jsonl`. Различать `CAPTURED` урок и действительно `APPLIED/procedure_used`; без независимого доказательства последнего не утверждать автоматическое применение. Идентификаторы уроков указываются в завершающем Git-коммите, следовать live readback.

## Нативное самообучение · фактический readback

**9 уроков сохранены штатным `air-worker learn event`, 9/9 ID и путей evidence проверены, RC0**. Строго через установленный AirWorker `0.11.11-rc.2`; журнал Git-tracked `learn/events.jsonl` изменён только нативным ядром, не ручным редактированием. Class+actor/source не повторяются. Source: `DevWork31-session-20261108`, actor `gpt-window:DevWork31`. Контрольный SHA всего журнала: `cd01c895ed8993177e8eb5ad23de54844be396dcb4bd4ca3ffe0b4f7629ca9d0`.

Native LR IDs:

- `LR-20261108T141453Z-828d4bc8` — Windows NTFS receipt temp filename limit, no false vendor failure;
- `LR-20261108T141617Z-82e57609` — exactly 2 Codex writers, coordinator patch SHA, targeted QA;
- `LR-20261108T141618Z-783d08c1` — consult accepted P0 receipts before new dispatch;
- `LR-20261108T141618Z-b7d2102e` — os.Root and O_EXCL across all sibling output writers;
- `LR-20261108T141618Z-5a6ce3fc` — live terminal DONE requires exit and bounded output SHA;
- `LR-20261108T141618Z-977237e5` — independent exact-SHA citations and fail-closed reviewers;
- `LR-20261108T141618Z-368054fc` — ordinary chat break alone is not native EXECUTION_UNKNOWN resume proof;
- `LR-20261108T141618Z-fa9d8084` — full package timeout not N124 hang; file symlink SKIP not PASS;
- `LR-20261108T141618Z-d7c84e90` — backup byte-exact WIP and native patch before scoped worktree cleanup.

Машинная квитанция с точными классами/ссылками: `docs/receipts/DEVWORK31_SESSION_LEARNING_20261108.json`, SHA `e29b86854c412665b3b90cf61fec4dfe5b23c745e841fa60fe84e24fd91385f1`. Сохраняемый нативный log: `D:/Devbox/AirWorker/evidence/DEVWORK31_SESSION_CLOSE_20261108/native_learn_event_readback.jsonl`. **Важное ограничение:** эти уроки `CAPTURED`, но НЕ доказано `APPLIED` и не создано `procedure_used`; самообучающая процедура не активирована без отдельной независимой проверки. Не записывать второй раз при продолжении.

## Первая проверка следующей сессии

Открыть **этот handoff** и верхнюю актуальную таблицу в `PLAN.md`. Сверить `git rev-parse HEAD`, `git status --porcelain`, `git ls-remote`, operational goal, 36 own worktree CLEAN, `learn/events.jsonl` и конкретные receipt SHA. Не создавать нового PLAN или веток, не дублировать source-only принятые микрошаги. Любые новые два Codex writer использовать только на **доказанный новый непокрытый** P0 блокер. Установленный продукт до отдельного release/install gate не менять.
