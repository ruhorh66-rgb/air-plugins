# Native AirWorker · два самостоятельных Codex-разработчика · bounded source correction

Фактический owner/координатор: текущая беседа ChatGPT Dev Work 30. Два Codex-воркера получают изолированные Git-worktree на одном source SHA, каждый имеет workspace-write только в своей копии. НИКАКОГО третьего Codex leader. Не коммитить, не пушить, не вызывать других агентов/модели, не трогать shared source, PLAN, судей, run-config, секреты или настройки Git. Координатор сам сверит две реальные правки и интегрирует их в общий исходник.

WORKER 1 (изменяет **только** `air-worker/cmd/orchestrate_coding_workers.go`):
1. Исправь duplicate-run guard для `prepareCodingWorktrees`: идентификатор собственной директории запуска должен зависеть от Git root + session scope + step, **но НЕ от меняющегося source HEAD**. После интеграции/нового коммита тот же `session+step` не должен создать вторую пару Codex job и затереть прежнее evidence. Старые пути не удаляй. Source SHA всё равно нужно проверять/сохранять в receipts.
2. Исправь машинную квитанцию `handoff.json`: поле `Report` внутри самого сохранённого JSON должно содержать фактический путь этого файла; сейчас в памяти устанавливается только ПОСЛЕ MarshalIndent. Сохрани безопасную запись и fail-closed при ошибке. Не меняй формат/семантику worker patch SHA.

WORKER 2 (изменяет **только** `air-worker/cmd/orchestrate_coding_workers_test.go`):
1. Расширь `TestChatGPTCoordinatorTwoCodexWorkersOwnRealEdits`: после ChatGPT integration сделай коммит в его **собственном синтетическом Git repo**, затем повторно вызови точно тот же native session+step с уже новым source HEAD. Подтверди `NOT_PROVEN`, отказ по существующему worktree и **0 новых jobs**. Ранее до интеграции уже есть duplicate-readback test — не удаляй.
2. Добавь проверку, что JSON по `doc.Report` реально хранит `Report==doc.Report`, обе роли — `coding-subagent/worker`, и ни один worker output не создаёт третью модельную job. Прогон исходной тестовой версии до интеграции с воркером 1 может быть RED: другой рабочий файл будет добавлен координатором после обоих результатов.

Оба воркера: ПИСАТЬ КОД САМИ в своих Git копиях, не возвращать текстовый diff вместо записи. Изменять только назначенный файл. Не переписывать исторические receipts и не отмечать P0 DONE. Сообщить точные changed files и результат; окончательная приёмка только после интеграции ChatGPT, Go unit/vet и независимого Anthropic review на новом чистом SHA.
