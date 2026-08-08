---
name: run-via-ruflo
description: Run a coding or shell task through an installed Ruflo/claude-flow runtime while enforcing a mandatory human approval gate before terminal execution and independently checking daemon state. Use when a user asks to orchestrate work with Ruflo, initialize a Ruflo swarm, or verify its daemon without waking it.
---

# Run via Ruflo

Use the real Ruflo CLI; do not implement a swarm or orchestration substitute.

## One swarm for the whole contour — не обсуждается

Состояние ruflo привязано к РАБОЧЕМУ КАТАЛОГУ: команда, поданная из другого cwd,
заводит там свой `.hive-mind` и `.claude-flow` и считает себя отдельным роем. Так
на 05.08.2026 в контуре оказалось **девять каталогов состояния при нуле живых
процессов**, три из них рапортовали `running: true`.

Канонический корень — `E:\-4-\ruflo-hive`. Он стоит умолчанием в `run_task.ps1` и
`install_ruflo.ps1`; передавать `-ProjectRoot` не нужно. Другой корень скрипт
**отвергает с кодом 5**, а не берёт молча: тихое согласие вернуло бы ту же россыпь.

Отсюда следует то, ради чего всё и делалось: **рой накапливает опыт в одном месте**,
а не начинает с нуля в каждом каталоге.

Две вещи, которые раньше путались и теперь разведены:

| | |
|---|---|
| `-ProjectRoot` | где живёт СОСТОЯНИЕ роя — всегда канонический корень |
| `-WorkDir` | где выполняется КОМАНДА — каталог проекта, по умолчанию текущий |

Перед прогоном `run_task.ps1` сам берёт замок и снимает его в `finally`, в том числе
после падения. Занятый замок живой сессии — команда **присоединиться**, а не поднять
второй демон: выход 4 означает «рой уже работает», а не «ошибка».

```
scripts\hive_single.ps1 -Action status        # где рой, жив ли, кто держит
scripts\hive_single.ps1 -Action sweep         # каталоги вне канона (только показ)
scripts\hive_single.ps1 -Action sweep -Fix    # ПЕРЕНОС их в archive, не удаление
```

**Перед любой уборкой** — `python scripts\consolidate_hives.py`. В каталогах роёв
лежат их ОБЪЕКТИВЫ, то есть постановки задач, которых больше нигде нет; сборщик
кладёт их в `E:\-4-\ruflo-hive\hives.sqlite` вместе с сырьём, по ключу SHA-256,
и повторный прогон ничего не задваивает. Удалять до слияния нельзя.

Каталоги ЧУЖИХ живых сессий уборка не трогает без явного `-IncludeForeign`: снять
историю роя у работающей рядом сессии — сломать ей работу, а не навести порядок.

## Проверка на утечку секретов — после каждого исполнения

Рой пишет файлы автономно, и никто не смотрит, не утащил ли он токен в рабочее
дерево. Инцидент этого класса в контуре уже был — ACL-экспозиция `openrouter.key`
на SRVLM01, 01.08.2026.

`run_task.ps1` прогоняет `scan_secrets.ps1` по `-WorkDir` в блоке `finally` —
**всегда**, в том числе после падения: упавший прогон успевает наследить не меньше
удачного. Результат дописывается в отчёт и НЕ подменяет исход самого прогона.

Сканер — `gitleaks` (8.30.1, установлен через winget), а не собственные регулярки:
свой набор шаблонов означал бы, что мы гарантируем то, чего не проверяли.

**Три исхода, ровно как требует `AIR_CONTROL.md`:**

| исход | код | что значит |
|---|---|---|
| `clean` | 0 | проверено, находок нет — рядом чем и за сколько |
| `leaks` | 1 | найдено — рядом файл, строка, правило |
| `unverifiable` | 3 | **ПРОВЕРИТЬ НЕ СМОГ** — рядом почему |

`unverifiable` — это **не «чисто»**. Нет сканера, отказ запуска, таймаут — всё это
даёт третий исход. Молчаливое превращение непроверенного в чистое и есть тот
дефект, ради которого модуль контроля вводили.

**Значение секрета не печатается никогда.** `--redact` включён жёстко: находка
называется местом и правилом, но не содержимым — в отчёте, в логе, в выводе.

Отдельно: `heycupola/relic` (IDEA-000071) для этой проверки **не годится** — это
zero-knowledge ХРАНИЛИЩЕ секретов, а не сканер. Он закрывает другую половину
задачи: если секрет лежит в нём, а не в `.env` рядом с кодом, рою нечего утаскивать.
Профилактика и контроль дополняют друг друга, подменять одно другим нельзя.

## Install only when needed

Run `scripts/install_ruflo.ps1`. It detects an existing `.claude-flow` installation or cached CLI and exits without changing it. For a fresh project, provide a trusted cached `claude-flow/bin/cli.js`; it runs the piloted non-interactive initializer. Do not alter the pilot at `E:\-4-\ruflo-pilot`.

## Canonical command, not our own orchestration

Переписано 05.08.2026 по решению ЛПР: контур не собирает `swarm_init`/`task_create` вручную —
используются штатные команды самого движка. Наша обвязка не придумывает роли/топологию.

### ИСПРАВЛЕНО 08.08.2026 — запуск состоит из ЧЕТЫРЁХ шагов, не одного

Прежняя редакция этого раздела утверждала, что `hive-mind spawn --claude -o "<цель>"`
«сама решает состав роя из цели». **Это неверно, и цена ошибки — все прогоны контура
с 05.08 по 08.08.2026** (`ERR-2026-000192`). Проверено по справке самого движка и по
официальному USERGUIDE:

```text
-n, --count   Number of workers to spawn   [default: 1]      <- состав задаём МЫ
task          Submit tasks to the hive                        <- шаг, который не вызывался
autopilot     Persistent swarm completion — keeps agents
              working until ALL tasks are done                <- шаг, который не вызывался
```

Состояние роя на момент находки: **18 воркеров, все `idle`, `Completed = 0` у каждого**,
Queen `Load 0.0%` с двумя задачами в очереди. То есть рой не выполнил ни одной задачи
ни разу — всю работу делала одна Claude Code сессия, поднятая четвёртым шагом, а
воркерам её никто не раздавал: их был один (дефолт движка), и очередь роя была пуста.

**Правильный порядок, зашит в `run_task.ps1`:**

| # | Команда | Зачем |
|---|---|---|
| 1 | `hive-mind spawn -n <N>` | завести N воркеров (параметр `-Workers`, дефолт 5 — из примера движка) |
| 2 | `hive-mind task -d "<цель>" -p <prio>` | положить задачу **в очередь роя** |
| 3 | `autopilot enable` | держать агентов, пока не закрыты ВСЕ задачи (отключается `-NoAutopilot`) |
| 4 | `hive-mind spawn --claude -o "<цель>"` | Queen-сессия, раздаёт работу воркерам через MCP |

### Не лезть в работу роя — но и не подменять её одной сессией

Указание ЛПР, повторённое несколько сессий подряд: рой должен работать **автономно**,
сессия не расписывает ему роли, топологию и порядок действий. Это верно — и именно
поэтому важно отдать ему задачу штатным способом (шаги 1–3), а не ограничиться
запуском координатора. Пропуск этих шагов — не «невмешательство», а превращение роя
в одну дорогую сессию, что и происходило.

**Проверка после запуска — обязательна и делается по статистике, не по самоотчёту:**

```text
cd E:\-4-\ruflo-hive
node <CliPath> hive-mind status      # Completed у воркеров должен расти
```

Если `Completed = 0` у всех и очередь не убывает — рой не работает, чем бы ни
отчиталась сессия.

## Одно окно — один рой на весь контур

`hive-mind spawn` не умеет исполнять в другом каталоге — состояние жёстко привязано к cwd
вызова. Поэтому рой **всегда** запускается из канонического `E:\-4-\ruflo-hive`
(`$ProjectRoot` в `run_task.ps1`), а реальный путь работы (`$TargetPath`) вписывается
**текстом в цель** — запущенная сессия сама переходит туда первым действием. Это даёт то,
что нужно контуру: одна очередь, один живой рой, не россыпь каталогов по проектам.

## Обязательная разовая настройка ПЕРЕД первым реальным запуском

`--claude` без регистрации MCP-сервера в `$ProjectRoot` тихо запускает не рой, а одну
обычную дорогую сессию без единого `mcp__claude-flow__*` инструмента — задокументированный
отказ (`005_Ruflo_Wiki`, «Пилот 04.08.2026»). Один раз для канонического каталога:

```
cd E:\-4-\ruflo-hive
claude mcp add -s project claude-flow -- node "<CliPath>" mcp start
```

Затем одобрить в `~/.claude.json` → `projects["E:\-4-\ruflo-hive"].enabledMcpjsonServers`
(добавить `"claude-flow"`) — либо один раз открыть интерактивную сессию в этом каталоге и
подтвердить запрос глазами. `run_task.ps1` проверяет присутствие в `.mcp.json` и отказывает
реальному запуску, если проверка не прошла (`exit 6`), но **не может проверить сам факт
одобрения** — это подтверждается только логом первого реального прогона
(`grep '"name":"mcp__claude-flow__' <лог>`), не самоотчётом сессии и не `claude mcp list`
(та команда показывает устаревшее состояние).

## Windows: движок ищет Claude Code юниксовым `which`, не `where`

Проверено 06.08.2026, дважды наступили. `hive-mind spawn --claude` перед запуском вызывает
`execSync('which claude')` (`@claude-flow/cli/dist/src/commands/hive-mind.js`) — юниксовую
команду, которой в Windows нет по умолчанию. Без нее движок пишет `Claude Code CLI not found
in PATH`, тихо переходит в режим "Falling back to displaying instructions" и **выходит с
кодом 0** — реальная сессия не запускается, а обёртка при этом ничем не отличает это от
настоящего успеха, если не проверять текст вывода отдельно (`run_task.ps1` теперь это делает
сам — ищет маркер деградации и возвращает `exit 7`, не `EXECUTED`).

`run_task.ps1` чинит это принудительным prepend в `$env:Path` перед вызовом:
`C:\Program Files\Git\usr\bin` (там `which.exe` от Git for Windows) и каталог с настоящим
`claude.exe` (`...\npm\node_modules\@anthropic-ai\claude-code\bin`, НЕ каталог с `.ps1`/`.cmd`
шимами — движок спавнит с `shell:false`, шимы не годятся). Если пути на конкретной машине
другие — поправить обе константы в начале скрипта, не удалять сам prepend.

## Разрешение на автономный запуск — Claude Code auto mode classifier

Отдельный от `permissions.allow` слой самого Claude Code: даже с точным разрешением на
`run_task.ps1` в `permissions.allow`, реальный запуск (`-Approval`) сессия **не может**
инициировать сама — классификатор блокирует «запуск автономного агента без надзора»
как класс действия, независимо от обёртки (проверено 05.08.2026 пятью разными способами
вызова, включая голую команду без единой строчки нашего кода — все заблокированы одинаково).

Снимается ТОЛЬКО ключом `autoMode.allow` в `~/.claude\settings.json` (уровень пользователя,
не проекта) — текстовое описание доверенного действия, не regex/путь. Правится ТОЛЬКО
человеком за консолью — попытка агента дописать себе же этот ключ **тоже блокируется**
классификатором (проверено 06.08.2026), и это ожидаемо: агент не должен сам себе выписывать
разрешение обходить проверку агента. Пока ключ не стоит — `run_task.ps1` с `-Approval`
исполняет только человек лично.

Обычный `permissions.allow` для `run_task.ps1` — держать версию кэша через wildcard
(`...\air-ruflo-bridge\*\skills\...`), не литералом: плагин обновляется, литеральная версия
в пути требует ручной правки на каждый бамп.

## Mandatory human gate

First run `scripts/run_task.ps1` **without** `-Approval` — mandatory `-Objective` and
`-TargetPath`. It calls `hive-mind spawn --claude -o "<цель+TargetPath>" --dry-run`, writes
the proposal (objective text, worker count, coordination prompt) to its report, then stops
with exit code 10. Claude Code is NOT launched at this stage.

Show the complete proposal/report to the human and wait for an explicit approval. Do not infer
approval from silence or from a prior request. Only after approval, re-run with the literal
`-Approval I_APPROVE_RUFLO_PLAN`. The script is the only path here that launches Claude Code
for real; it refuses to before the gate, and refuses again (`exit 6`) if MCP registration is
missing.

**Why the real run does NOT pass `--no-auto-permissions`:** the engine's own default
(`--dangerously-skip-permissions: true`) exists so a headless launch doesn't hang on an
internal prompt nobody can answer. Our external dry-run-then-explicit-approval gate already
supplies the human checkpoint that internal prompt would have provided — disabling the
engine's default here would just make the approved run hang, not make it safer.

Never say roles were actually distributed merely because Ruflo proposed them or `hive-mind
spawn` printed a worker table. Require evidence — grep the run's log for
`"name":"mcp__claude-flow__"` — before making that claim; otherwise say only “roles proposed”.
Likewise, do not report a successful run solely from Ruflo output — verify the requested
artifact/test independently.

## Daemon status without waking it

Do not call Ruflo's daemon-status command for a simple status check: it can start workers. Run `scripts/verify_daemon_state.ps1` instead. It reads the state file and compares its PID to `Get-CimInstance Win32_Process`; it reports exactly one of `confirmed`, `contradicted`, or `unverifiable`. Treat `unverifiable` as unknown, not stopped.

## Цикл

```text
цель   — рой отработал и НЕ оставил секретов в каталоге
гейт   — skills\run-via-ruflo\scripts\scan_secrets.ps1 (clean=0 / leaks=1 / unverifiable=3)
предел — 2 круга: утечка чинится один раз, второй отказ несётся ЛПР
показ  — один итог: что запускалось, вердикт сканера, файл отчёта
```

**`unverifiable` — не «чисто».** Отсутствие сканера, отказ запуска и таймаут дают
третий исход, а не первый, и цикл на нём не закрывается.

Канонический текст цикла — скилл `verification-loop` (плагин `air-loop`, `E:\-7-\air-loop`). Раннер, считающий круги: `E:\-7-\air-loop\scripts\loop_run.py`.
