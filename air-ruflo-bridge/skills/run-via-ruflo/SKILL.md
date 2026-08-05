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

## Mandatory human gate

First run `scripts/run_task.ps1` **without** `-Approval`. It may call only `swarm_init` and `task_create`, writes the proposed topology, role assignments, task descriptions, and budget (`maxAgents`) to its report, then stops with exit code 10.

Show the complete proposal/report to the human and wait for an explicit approval. Do not infer approval from silence or from a prior request. Only after approval, re-run with the literal `-Approval I_APPROVE_RUFLO_PLAN` and an explicit `-Command`. The script is the only path here that calls `terminal_execute`; it refuses to call it before the gate.

Never say roles were actually distributed merely because Ruflo proposed them or `task_create` succeeded. Require evidence of distinct Ruflo agent IDs and distinct OS process IDs/calls before making that claim; otherwise say only “roles proposed”. Likewise, do not report a successful run solely from Ruflo output—verify the requested artifact/test independently.

## Daemon status without waking it

Do not call Ruflo's daemon-status command for a simple status check: it can start workers. Run `scripts/verify_daemon_state.ps1` instead. It reads the state file and compares its PID to `Get-CimInstance Win32_Process`; it reports exactly one of `confirmed`, `contradicted`, or `unverifiable`. Treat `unverifiable` as unknown, not stopped.
