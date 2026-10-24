# HLP-03 — реальный reviewer на заранее фиксированном корпусе

## Разрешённый объём
ЛПР 24.10.2026 подтвердил продолжение HLP-03 и применение смягчённого DEV-137 v1.1.0. Работа только на SRVLM01 в существующем checkout.
Source root: R:\-4-\air-worker\worktrees\airworker-learning-loop-20261024\air-worker
Base HEAD: 95a4377f35948f9396d59cfe86d1e1e5e29e2979.
Текущая задача исполнителя: добавить минимальный opt-in оценочный тест для РЕАЛЬНОГО production reviewer; реальные вызовы провайдера в этом исполнительском шаге НЕ запускать. Координатор заморозит корпус/labels и запустит отдельно.
Создать только cmd/learn_adapter_live_eval_test.go (и при необходимости короткую документацию запуска в этом task-файле). Не менять production code, существующие тесты и исходную политику prompt. Не создавать framework/БД/индекс/очередь.

## Сохранение состояния
Один writer. Native PLAN.md, plan/**, release-slices, LEARN, goal/**, run-config.json, зависимости, installed product, live learning owner/runtime, службы, глобальные настройки и весь чужой WIP сохраняются. Не commit/push/release/install, не новые native N-ID, не AIR-ENV-002.
Использовать существующие task GOCACHE/GOTMPDIR/TEMP/TMP и session root из окружения. Старые 512 MiB и остаток 43 MiB не являются действующим лимитом этого задания. Для этого внешнего QA принята рабочая оценка до 2 GiB совокупного собственного cache/temp с наблюдением; предупреждение и пересмотр при выходе за оценку, без kernel quota. Сохранять резерв общего тома 10 GiB/10%, 1 run, deadline и bounded IO. Не чистить shared cache, не удалять старые доказательства.

## Минимальный механизм
Читайте cmd/learn_adapter.go, cmd/learn_shared.go, cmd/learn_adapter_reuse_test.go и fixture helpers. Используйте существующий sharedProductFixture(t,false), reuseSeed/reuseIndex и штатный runLearningProcess; не selfLearningFixture, который включает helper reviewer.
Production путь: runLearningProcess -> source air-worker.exe learning-adapter reviewer -> invokeLearningReviewerCodex -> native Codex receipt -> actual model answer -> actual adapter -> executeSharedLearning propose/apply в изолированном fixture.
Запрещено подменять learningReviewerInvoke ответом, напрямую вызывать Codex/HTTP или подменять production prompt. Нужен полный путь с настоящим provider response, source binary берётся из env с проверкой его SHA.
Текущий installed reviewer config read-only: model=gpt-5.6-luna, effort=medium; adapter timeout 240000ms. Проверять именно этот выбор; model/effort берутся из явно переданных env и пишутся в evidence, молчаливого fallback нет.
Переиспользуем Hermes existing-skill-first, fresh full read, update exact target/preSHA, module-owned apply/backup/rollback. Это уже внедрено в production adapter; новый code path не нужен.

## Opt-in тест и вход
TestHLP03RealReviewerCorpus по умолчанию Skip. Env opt-in AIR_WORKER_HLP03_LIVE=1. Прочие env: AIR_WORKER_HLP03_CORPUS, AIR_WORKER_HLP03_OUTPUT, AIR_WORKER_HLP03_BINARY, AIR_WORKER_HLP03_BINARY_SHA256, AIR_WORKER_HLP03_MODEL, AIR_WORKER_HLP03_EFFORT.
Входной JSON: {"schema":"air-worker.hlp03-corpus-input/v1","cases":[{"id":"C01","procedures":[{"target":"skills/learned/...md","body":"# ...\n\n## When to apply\n...\n\n## Procedure\n...\n\n## Pitfalls\n...\n"}],"event":{"kind":"failure","class":"...","observed":"...","outcome":"..."}}]}.
Максимум 24 cases, ID unique, корпус <=256 KiB, тела <=64 KiB, evidence bounded. Labels/expected отсутствуют во входе; никакого scorer с золотыми ответами в model prompt. Каждому case отдельный чистый fixture. Seed только через штатный module propose, не forged ledger. У каждого case собственный изолированный hook state. Пакет содержит {"event":case.event}; input не содержит expected label.
Перед вызовом собрать тот же reviewerCatalog/prompt существующими функциями, сохранить prompt/context SHA и текст (сырой prompt и точный ponytailPrompt, отправляемый native runner), corpus SHA, binary SHA, config SHA, case packet и seed catalogue.
Реальный вызов выполнить через runLearningProcess с фиксированными executable/SHA, Args learning-adapter reviewer -product fixture -model MODEL -effort EFFORT и timeout <=240000ms. Общий тест ограничен запускателем. Не менять active live self selector.
Сохранить adapter raw output или отдельную ошибку, elapsed, native .woody/jobs receipt и bounded .out до автоматического удаления t.TempDir. Из raw Codex JSONL сохранить usage (если нет — null), actual model metadata, terminal outcome и число/типы tool calls. Reviewer должен решать по данному prompt; любые обращения к файлам корпуса/labels/source считать contamination, не чистой семантической оценкой. Не выдумывать receipt exit code: неизвестное остаётся unknown.
Различать фактическое no_candidate (валидный {} при успешном adapter), update (pre_sha256 непустой), create, provider/adapter error, malformed/refusal/timeout. Ошибка не является no_candidate. Никаких retries.
Для валидного actionable candidate использовать только existing executeSharedLearning propose в fixture. Сверить module outcome и before/after catalogue, сохранность всех несвязанных seed процедур. Для update проверить точный прежний target/preSHA и backup; rollback через module в fixture после фиксации результата, если соответствует штатному API. Для create подтвердить +1 и сохранность остальных. Измеряется lifecycle, не production benefit.
Результат каждого case отдельный малый JSON плюс summary.json с corpus/model/config/source identities и все 12 попыток. Semantic labels координатор сравнит отдельно после всех ответов; тест не должен заявлять semantic PASS из технич. RC0.
Сохранять нужные evidence вне удаляемого fixture root. Сохранение неполных результатов — до Fail/timeout где возможно. t.Cleanup/t.TempDir штатно чистит только текущий собственный fixture; сохранять cleanup статус честно.

## Проверка этого исполнительского шага
gofmt нового файла. go test . -run '^TestHLP03RealReviewerCorpus$' -count=1 -timeout 120s БЕЗ live env: должен компилироваться и SKIP без provider call. Можно добавить одну маленькую offline проверку сериализации/классификации если есть нетривиальный helper.
Не запускать широкий regression, go vet/build и live провайдер в этом шаге. Вернуть изменённые пути, точную команду и код, схему output и ограничения. Реальный evaluator запуск и независимую проверку делает координатор.
