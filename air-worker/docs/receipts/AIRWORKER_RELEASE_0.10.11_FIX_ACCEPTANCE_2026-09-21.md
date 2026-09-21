# AirWorker 0.10.11 (исправленный) — квитанция приёмки кампании

Дата: 2026-09-21 15:45 (SRVLM01)
Исходный коммит сборки (S): `454aedae2826917bd3bc6d466b3d601dadaa7731`
Версия бинарника: air-worker 0.10.11
Исполнитель шагов с кодом: Codex (gpt-5.6-luna), смысловой судья: Claude. Оркестрация: Aworker.
Публикация, тег, Release, marketplace, установка: не выполнялись, требуют отдельного слова ЛПР.

## Артефакты

| Файл | Байт | SHA-256 |
|---|---:|---|
| `bin/air-worker.exe` | 7669760 | `ad3f794bb0165aa4eda71a9572101df09dd345902c894f5f8280a6decf96fcac` |
| `bin/air-worker-tray.exe` | 2502656 | `dca49aba7d3a2db60168ec65dd2ef0c76aa5a1add2fe773fdfa51463230d661b` |

## Результаты кампании

- Полный прогон (vet, go test, Python-тесты плагина, check-plugin): OK
- Стенд гонки квитанции: polled runs 30, ok 30, lost 0, aborted 0; held-receipt runs 10, ok 10, bad 0
- Мутация M6: test passes on original: True; mutations applied: 2; test fails on mutant: True
- Сценарии Ц7: gate barrier x3 and PSModulePath scenario hold
- Мутации Д-5: both D-5 mutations killed by their tests

Независимая сверка v2 и подпись куратора — гейт 12 плана кампании.