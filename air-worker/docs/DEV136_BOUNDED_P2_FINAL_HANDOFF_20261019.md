# AirWorker DEV-136 — ограниченный кеш, закрытие двух P2

**19 октября 2026 SGT · SOURCE-ONLY JUDGED PASS (P0/P1/P2=0) · N124 OPEN · DEV136 -apply HOLD.**

## Принятые исходники

AirWorker line A, независимая source ветка `dev/airworker-dev136-bounded-p2-a-20261019`. Изолированная база [`c8f1857`](https://github.com/ruhorh66-rgb/air-plugins/commit/c8f1857ad5bd2f27278c6ea54c02cfac627c70e2). Изменения кода `af2f706`, окончательная исправленная ревизия `acc7d697e9a980f54941918cb4fbd887ac5ba23f` — только `plan_header_payload_bounded.go` и `plan_header_payload_p2_test.go`. Ни общий `distribution_identity.go/payloadSnapshot`, ни PLAN/N/LEARN/установленная 0.11.8 не изменялись.

Судья `gpt-6-sol medium` ранее дал source-only PASS с двумя P2: mutable package variable для максимальных лимитов и тест sparse-файла, не проверявший правильную причину отказа. В окончательном source они закрыты:

- Шесть неизменяемых компиляционных ограничений (8192 записей, 4096 файлов, 256 МиБ всего, 128 МиБ на файл, глубина 32, относительный путь 2048) действуют независимо от изменения переменной defaults.
- Отрицательные тесты используют `MaxInt64`, `MaxInt` и повышенные счётчики: расширение лимитов отклоняется.
- Настоящий sparse-файл `128 МиБ + 1 байт` отклоняется именно по `per-file byte limit`.
- Настоящий файл растёт при чтении через открытый дескриптор: дополнительный байт обнаруживается, функция возвращает ошибку; сокращённый поток тоже отклоняется.

## Проверенный RED / GREEN / независимый судья

| Стадия | Факт SRVLM01 | SHA256 журнала |
|---|---|---|
| Предыдущий код, настоящий RED | 6 вложенных FAIL, RC1 | `69b8fe98b99aeccd30beeea992b5eb429bce599a098b402447cb76db9af4ef43` |
| P2 first GREEN | 6 main + 13 nested PASS, RC0 | `c1de34c0eaa1afd27c0d084880343fd1d15dd76e1be016757191ddeac1032cc4` |
| Константные лимиты, final GREEN | 7 main + 13 nested PASS, RC0 | `4f9db486030f983b23bb36218b86042dd1780f514fc101e3c088d82a76d464b2` |
| `^TestPlanHeader` | **43 основных + 97 вложенных PASS, 3 SKIP, RC0** | `c52a43f94111e2e1cc3adf2929d32f34da77d70b268b36171da475e144946e9d` |
| Реальный Claude 0.11.8 cache | 489 файлов, bounded/native SHA совпадают | `f3abb659dc4c79b26bcac548c588a821f1100d7b6280f8e03fcf5fe1e1d467f5` |
| Реальный Codex 0.11.8 cache | 489 файлов, bounded/native SHA совпадают | `d49760cd149b4e4d746e03e5fff5902a320d59716433fcdb966628a8c1b3ac47` |

У двух установленных кешей одинаковый исходный digest: `462441dc54438e5f322b700adace27cfd8061bb157932f44296ff6c2be0a18ed`. `go vet`, source-only `go build`, нативные `plan-lint` и `validate`: RC0. Собранный EXE только в `R:`, SHA `efcbdf06476b2c1fa2820b76cac55145d0497c735fe646736bb58aee80bc8eb3`.

**Независимый read-only Codex `gpt-6-sol medium` на SHA `acc7d697`: PASS, P0=0, P1=0, P2=0, RC0.** SHA вердикта `9f45e748957321411db311e031329f022a98da8045553572f58f87903d589f65`. Судья исследовал frozen source и предоставленные журналы, но не запускал тесты; полная Go-регрессия всей библиотеки NOT_PROVEN.

## Производственные границы и координация B

Установленная AirWorker **0.11.8** SHA `1b4008af05110214934d46c0ab8c5236341cd1c699f54efb895c2f9f64a94a94` не менялась. N-124 остаётся OPEN, DEV-136 `-apply` BLOCKED, AirLegal N-111 OPEN. Подписанное происхождение релиза и глобальный generation CAS не входят в этот источник.

Worker B продолжает R03 (девять WIP-файлов); N093 OPEN в её рабочем плане, CLOSED только в отдельной интеграционной ветке A [`be680c8`](https://github.com/ruhorh66-rgb/air-plugins/commit/be680c80823f02e7e59b3cb0184080f7ea38f1a6). Не переносить нативные PLAN/N/LEARN вручную: после B checkpoint нужен один core writer.

**Owner:** A, curator Voody. **Trigger:** два P2 от независимого судьи. **Done when:** константные лимиты + реальное изменение файла + sparse-specific error + целевой QA и независимый frozen-source PASS. **Judge:** Go host + Codex Sol medium. **Receipt:** [машинная квитанция](receipts/AIRWORKER_DEV136_BOUNDED_P2_FINAL_ACCEPTANCE_20261019.json). **Rollback:** revert только feature commits, установленная 0.11.8 сохранена.

**Следующая веха:** подтверждение machine active release и N-124 generation CAS существующим AirWorker core; release/tag/install/UAC/reboot/остановка чужих служб только по отдельным гейтам ЛПР.
