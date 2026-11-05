# Dev Work 26 → Dev Work 27 · AirWorker 0.11.11-rc.4 / N-130 · 05.11.2026 SGT

**Статус блока:** SOURCE-ONLY HANDOFF / релиз не установлен / N-130 OPEN. Хэндофф принят к подготовке по прямому указанию ЛПР «сохранить наработки, обучить базу самообучения, дерево CLEAN, commit/push». Этот файл не является решением ЛПР о завершении P0 или прохождении релизной приёмки.

## 1. Точка входа новой сессии

- Только AIR Commander с явным `node=SRVLM01`. Перед действиями: установленный `air-worker version`, штатный GitHub AirWorker skill (`skills/woody/SKILL.md`), `air-worker plan spine -product <root> -json`, `air-worker plan node list`, `plan-lint`, факт Git. Не пользоваться Codex App Server/прямым Claude API. Никаких ручных правок `PLAN.md`, `plan/N-*`, `learn/events.jsonl`: только родное ядро.
- Рабочий продукт: `D:\Devbox\AirWorker\worktrees\awlrn-orch-pilot-20261104\air-worker`. Git-репозиторий в родительском каталоге, ветка `dev/airworker-native-orch-pilot-20261104`. Перед текущим сохранением HEAD был `771a4df1c3e5a52e63823cde1a354f44448c215b`. **Сначала проверить последний Git HEAD после сохранения** и совпадение local/upstream/origin; не возвращаться к старому SHA.
- Wiki: `F:\-5-\011_Plugins\AirWorker_Wiki`, ветка `dev/airworker-curator-0.10.14`. Последний исходный Wiki HEAD перед сохранением `1f2f49b1bfd1ee599771e13da00ce1bcb6a884f0`.
- Установленный на SRVLM01 бинарник **`0.11.11-rc.2`**, SHA-256 CLI `495D1D274C04AAE41189579E368356C9A15BCB1C057227DB538C93AA41ACEFBE`; tray `C8A12B362975349C3C376E830B61F5858ED3C3439650374DC56F53F40442F67B`. Два хост-кэша Claude/Codex rc.2 сверены с installed. **Release `0.11.11-rc.4` ещё НЕ опубликован и НЕ установлен.**
- Нативная сессия текущего воркера: `gpt__DevWork26Native20261104`. В новой обычной GPT-сессии объявлять **собственный** session-key, чужой не присваивать.

## 2. Приоритет и нативная нить

1. **`N-130_rc4-release-p0-source-reviewer-fixes-and-transac` — OPEN.** Триггер: ЛПР предварительно разрешил публикацию и установку; независимый Codex `gpt-6-sol:medium` прочитал исходники и вернул **FAIL_P0**. Задача — устранить два P0, транзакционный откат кэшей, затем независимый повторный review на новом exact SHA, утверждённый релиз и официальный update с подписанным Ed25519-каналом. Не закрывать N-130 по успешной локальной регрессии. Parent N-129 OPEN.
2. `N-131_dev-work-26-session-save-learning-and-handoff` — отдельный узел сохранения: закрыть только после нативных квитанций обучения, коммитов, push и двух CLEAN деревьев.
3. `N-126` (настоящие **два параллельных** Codex read-only субагента → leader → независимый Anthropic) и Hermes-parity P0 (живой review/create/refine/load в следующей сессии) **OPEN**. Исторический `R0.11.9-05` запрещает фактическую оркестрацию; не удалять sidecar, не делать numeric fallback и не считать два последовательных dispatch за два субагента.
4. ЛПР разрешил публикацию и установку заранее, **но не разрешал UAC, reboot, остановку чужих служб или обход подписывающего ключа**. Гейт подписи/фактической QA сохраняется.

## 3. Независимая проверка, исправления и точная QA

- Исходная независимая задача: `docs/tasks/DEVWORK26_RC4_INDEPENDENT_CODE_SECURITY_REVIEW_20261105.md`, native Codex job `gpt__DevWork26Native20261104__1793840034261393400__3cdb9b06` на Sol medium. Терминальный `RC0` означает работу исполнителя, **содержательный вердикт `FAIL_P0`**:
  - **P0 1**: `cmd/bypassguard.go::classifyBypass` преждевременно возвращал ALLOW после допустимого `claude plugin marketplace add`, что скрывало последующий `codex exec`. Исправлено: проверяются все marketplace-аргументы, разбор прекращается на `; & |`, затем выполняются остальные guards. Положительные и отрицательные случаи в `cmd/bypassguard_test.go::TestN130CompoundAuthorizedMarketplaceCannotMaskLaterBypass`.
  - **P0 2**: `cmd/semantic.go::invokeSemanticClaude` принимал валидный `PASS` JSON при ошибочном exit code процесса. Исправлено через `parseClaudeSemanticResult(raw,runErr)`: nonzero process exit всегда ошибка, даже если JSON выглядит как PASS. `cmd/semantic_process_exit_test.go` покрывает nonzero, пустой ответ, `is_error`, нормальный ответ.
  - **P1**: штатный `cmd/update.go` синхронизировал host plugin caches **до** lock/apply, а `cmd/update_windows.go` откатывал только CLI/tray. Новое source-only WIP: `cmd/update_cache_transaction_windows.go` snapshot metadata Claude/Codex, старых bin SHA и отсутствия target-version; vendor sync перенесён внутрь locked native `_apply` с rollback обоих кэшей. `cmd/update_cache_transaction_windows_test.go` проверяет normal rollback, отсутствие чужих удалений, foreign/cache-downgrade deny. **Риск Windows reparse/junction требует независимой проверки; тест симлинка SKIP из-за отсутствующих привилегий. Ни одного live cache/install изменения не было.**
- На текущей незакоммиченной дельте `gofmt RC0`, `go test . -run '^(TestN130|TestUpdatePair)' -count=1` **RC0**, `go vet ./...` **RC0**, `air-worker plan-lint` **RC0**. **Один тест `TestN130RollbackRejectsLinkedCacheTarget` — SKIP**, не PASS. Полный Go suite после N130 **не запускался**.
- Ранее full Go PASS `RC0` (main 921.190s, tray PASS) относится только к source SHA `9e53ebc`, и targeted+vet RC0 — к `b659d86`; НЕ подменять ими full QA нового SHA.
- Код rc.4 до N130: `b659d8684b96662567cc53290019af8ca92c3b98`. На этом SHA Claude vendor validate --strict RC0/0 warnings, Codex startup+plugin check PASS, Hermes Python unit **31/31 PASS** и profile **4/4 PASS**. Эти проверки **не охватывают новый N130 код**. Portable ZIP `air-worker-v0.11.11-rc.4-N129-final-candidate.zip` SHA `9AB3D225...` и VCS payload ZIP SHA `D7AB2779...` — исторические source-only артефакты **НЕ устанавливать**, потому что после них изменён код.
- Для релизного Go build обязателен настоящий Git-клон, где `.git` — каталог: междисковый worktree (D: исходники/F: gitdir) давал отсутствующий `vcs.revision`. Чистый clone `b659d86` дал `vcs.revision=b659d86`, `vcs.modified=false`, asset CLI SHA `94025DE7...`, tray SHA `2CC15989...`. Эти assets также **устарели после N130**, собирать новые по точному итоговому SHA.
- Штатный `air-worker update` проверяет signed Ed25519 manifest и `vcs.revision`; источник `tools/publish-github-update.ps1`, ключ `air-worker-update-2026-01`. **Нужен владелец существующего signing key, приватный ключ НЕ передавать в чат, не создавать новый trust root, не обходить запреты OpenAI на инструментальную операцию с ключом.** Ранее операция передачи `-PrivateKey` блокировалась. GitHub release `air-worker--v0.11.11-rc.4` ещё не существовал при проверке. Сначала CODE REVIEW / QA, затем штатная подпись/публикация, далее установка.

## 4. Самообучение текущей сессии

Через штатное `air-worker learn event -product <D root> -actor-kind gpt-window -actor DevWork26 -json` записано **шесть** типизированных событий `RC0`; не редактировать журнал вручную:
- `LR-20261105T033806Z-996cea46` — правильная Claude subscription auth через user-env OAuth+Headroom (не голый `claude auth status`).
- `LR-20261105T033844Z-e3929321` — судья nonzero exit не может принять даже корректный PASS JSON.
- `LR-20261105T033844Z-933039f7` — полный разбор compound shell после разрешённого marketplace.
- `LR-20261105T033844Z-d8dea8f3` — атомарный откат собственных caches и metadata, SKIP линк-кейса не PASS.
- `LR-20261105T033844Z-8de50215` — clean Git provenance и существующая Ed25519-подпись.
- `LR-20261105T033845Z-504df06b` — версия тестов, точная квитанция и `EXECUTION_UNKNOWN` без слепого повтора.

Запущен штатный `air-worker learn review -product <D root> -session DevWork26Native20261104` **по последним событиям, без настоящего transcript файла**. На момент создания этого текста итог ещё не подтверждён: сверить `.air-worker/learn/reviews/` и терминальную квитанцию текущего запуска, не инициируя дубликат. Если появятся предложения, это максимум PENDING; **новый одобренный/applied навык не доказан**. `air-worker learn self status -json` показывает self-owner **ENABLED, но selector ссылается на старое R: worktree** `R:\-4-\air-worker\worktrees\airworker-0119-n093-worker-b-20261018\air-worker`; не менять селектор без отдельной проверки и нативного transfer-owner, не выдавать записи D: за успешное live auto-load R:.

## 5. Границы передачи и ближайшее действие Dev Work 27

- Начать со сверки source/Wiki HEAD, статуса `N-130`, `N-131`, exact task/LEARN native receipts, `gofmt`/targeted QA (принятое **не дублировать без дельты**), и проверки двух CLEAN Git деревьев; на SRVLM01 только через AIR Commander с явным node.
- Код N130 сохранён как **WIP SOURCE_ONLY**. Перед публикацией нужен независимый Codex/Anthropic source/security review именно итогового SHA: особенно **NTFS reparse/junction и incomplete rollback**. Затем только строго адресная новая QA и freeze, новый VCS-stamped CLI/tray, Claude/Codex/Hermes, signed Ed25519 publication через штатный owner, официальный `air-worker update`/rollback.
- Live `F:\-7-\air-worker\run-config.json` не имеет новых `require_native_subagents` и `require_anthropic_reviewer`. Подготовленная **preview-only** миграция двух полей SHA `DFF61492E06D83713014A632776D2D63AB8557FD56FB271411AAAFE6EF61DBFC`; исходный live SHA `3C7A9D7D3D6C4B7E9D90326C8AEA6CF52137A1F65A4D6D57E11AF72CFE051182`. Не применять в обход релиза; N130/релизная транзакция её должна включать.
- Не делать installer, UAC, reboot, чужие service stop в ходе session-save; signer blocker и runtime N126 не скрывать. Только после релиза перейти к P0 автообучению: реальный `event→review→safe create/refine→SHA ledger→next relevant session skill_loaded`.

**Порядок сдачи:** нативные learning IDs → квитанция save → Git commit/push source → Git commit/push Wiki → проверки local/upstream/remote/CLEAN → нативное закрытие **только N-131** при выполненных критериях → финальный source commit/push/readback.
