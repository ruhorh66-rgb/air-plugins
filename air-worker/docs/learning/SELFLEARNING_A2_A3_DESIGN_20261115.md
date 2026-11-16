# Самообучение AirWorker · проект А2 и А3 · 15.11.2026

Исполнитель: Dev Work 35. Статус: **проект**, кода нет. Код — штатным маршрутом
(пишет Codex, судит Anthropic), применение на живом LEARN — по гейту ЛПР.
Плановые узлы: А2 → N-110, А3 → N-110 (см. `SELFLEARNING_AB_NODE_REQUESTS_20261115.md`).

## А2 · отказы ревьюера

**Факты** (рантайм `R:/-4-/air-worker/work/AIRWORKER_0119_R02_PRIMARY_20261020/runtime`,
разборы с 08.11.2026, связь с версией захватившего бинарника по `run_id`):

| Версия | Исход | Число | Период |
|---|---|---|---|
| 0.11.11-rc.5 | reviewer_failed | 492 | 08.11 06:31 … 15.11 08:52 |
| 0.11.11-rc.2 | reviewer_failed | 488 | 15.11 07:12 … 09:29 |
| 0.11.11-rc.2 | reviewer_context_limit | 221 | 08.11 … 11.11 16:01 |
| не CLI (STOP и пр.) | reviewer_failed | 85 | 13.11 … 15.11 |
| 0.11.9 | reviewer_failed / candidate / no_candidate / adapter exit 2 | 8 / 2 / 2 / 4 | 08.11 … 15.11 |

Каждый run разбирается до трёх раз (414 run × 3, 32 × 2).

**Выводы.**
1. `reviewer_context_limit` давал только rc.2 (лимит 32 процедуры при 43 APPLIED). В rc.5
   лимит 64 / 128 KiB, каталог 63 КБ — после 11.11 не встречается. Второго каталога нет.
2. `reviewer_failed; detail withheld` — stderr ревьюера с префиксом `learning reviewer: `,
   не совпавший с известными диагнозами (`learningFailureReason`, `learn_shared.go`).
   Последний вызов Codex ревьюером — 09.11.2026 05:13 (квитанция
   `.woody/jobs/legacy-default-learning-learning-reviewer.receipt.json`, DONE). Значит, все
   отказы после — **до вызова модели**. rc.2, который до 11.11 доходил до проверки каталога,
   теперь падает раньше неё: остаются `normalizeLearnProduct` (`learn_adapter.go` ~130) и
   `readSharedLearningSettings` (~135). Точный текст не установлен: изолированный запуск
   `learning-adapter reviewer` с пакетом `{}` в сессии Dev Work 35 запрещён проверкой прав.

**Минимальное исправление.**
- А2.1 · стадия отказа: каждое `Fprintln(os.Stderr, "learning reviewer:", err)` в
  `cmdLearningAdapterReviewer` получает безопасный префикс стадии
  (`stage=product|settings|input|catalog|invoke|answer|apply`); `learningFailureReason`
  возвращает `reviewer_<stage>_failed` без текста ошибки. Тест на каждую стадию; секреты по
  существующему правилу не выводятся.
- А2.2 · без повтора детерминированного: отказы стадий `product`, `settings`, `catalog`
  не ретраятся (одна попытка, квитанция). Повторяются только `invoke` и занятость.
- А2.3 · окружение (гейт установки): события до сих пор пишут rc.2 и 0.11.9; убрать старые
  версии из используемых кэшей плагина после выпуска.

Judge: машинный подсчёт `reviews` по исходам за 7 дней после установки; доля `error` < 10%.

## А3 · один класс — одна процедура

**Причины дублей** (32 файла `air-worker-cli-failure-2653180e-*`):
1. Код: при пустом `update_target` адаптер создаёт новый файл
   `<класс>-<sha16(content)>.md` (`learn_adapter.go:288-292`), даже если процедура того же
   класса уже есть. Схема ответа допускает `update_target: ""` всегда
   (`reviewerOutputSchema`).
2. Вход: ревьюер получает вердикт air-modules `unjudged / judge not configured`, который
   пишется на каждое событие `run_completed`, и учит «как обращаться с ненастроенным
   судьёй».

**Исправление (код → поведение CLI → текст навыка).**
- А3.1 · схема: если в каталоге есть update-eligible процедура того же семейства класса
  (`target == classTarget` или префикс `<класс>-`), `update_target` в схеме — только эти
  цели, без `""`. Модель обязана патчить существующую; новый файл семейства не создаётся.
  Новое семейство — по-прежнему новый файл. Тесты: семейство есть → новый файл невозможен;
  семейства нет → создаётся; protected target → отказ.
- А3.2 · вход ревьюера: вердикт с `status=unjudged` и `reason="judge not configured"` в
  пакет ревьюера не передаётся как материал урока (поле заменяется на
  `{"status":"unjudged","configured":false}`), air-modules не меняется. Отдельная заявка
  на air-modules — `AIR_MODULES_UNJUDGED_VERDICT_REQUEST_20261115.md`.
- А3.3 · CLI: `air-worker learn events`/`learn self status` показывают для таких
  событий «судья не настроен → прогон не оценён» из кода; навык для этого не нужен.
- А3.4 · типизированный FAIL судьи (инв. 15, решение куратора 15.11): в
  `ownCLITrustedFailureCause` для `judge` с кодом 1 читать `<product>/.goal-verdict.json`
  (пишет `publishVerdict`, `judge.go:933`) — только если `code == 1` и `at` не старше
  начала процесса. Причина `judge-criteria-failed`; observation содержит только ID
  упавших критериев (`criteria_failed`, санитизированные `[\pL\pN._-]`, не более 16) и
  `checks_failed`; `outcome_ref` = путь `.goal-verdict.json#sha256=…`. Свободный текст
  скриптов не копируется. Тесты: свежий вердикт → типизированное событие; устаревший или
  чужой → обычное событие кода 1; подмена файла после записи → отказ повтора.

**План свёртки 32 дублей на живом LEARN (по гейту ЛПР).**
1. Снимок: копия `skills/learned/air-worker-cli-failure-2653180e-*.md` и
   `ledger.jsonl`/`proposals.jsonl` с SHA-256 (readback).
2. Выбрать одну каноническую процедуру семейства (самую раннюю APPLIED).
3. Остальные 31 — штатными `learn rollback <proposal_id>` (каждый со своей квитанцией),
   а не удалением файлов; ledger фиксирует откат.
4. Readback: `learn index` — одна процедура семейства; число APPLIED 43 → 12.
5. Откат свёртки: `learn approve/apply` тех же proposal_id из снимка (они в ledger),
   либо восстановление снимка, если штатный повтор невозможен.
Без А3.1 свёртка бессмысленна: дубли появятся снова.

## Маршрут кодинга

`run-config.json` продукта: `orchestration.enabled=true`, `subagents=2`,
`require_native_subagents=true`, `require_anthropic_reviewer=true`; контракт ЛПР 07.11:
координатор — беседа ChatGPT, два Codex worker в своих worktree, интеграция — ChatGPT,
затем factual QA и независимый Anthropic. Из сессии Claude Code (Dev Work 35) роль
координатора по контракту не совпадает; запуск стартует два платных Codex-задания и пишет
рабочие деревья. Не запускался. Нужно решение: координировать из GPT-окна, либо разрешить
Claude-координатора для этого шага.
