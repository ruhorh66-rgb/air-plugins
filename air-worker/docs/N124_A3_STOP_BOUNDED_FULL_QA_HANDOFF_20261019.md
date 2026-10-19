# AirWorker N-124 · bounded Stop/reviewer · независимая приёмка исходников

**19.10.2026 SGT · SRVLM01 · SOURCE-ONLY FULL GO PASS + Sol medium P0/P1/P2=0 · N124 production OPEN**

Исправлен воспроизводимый отказ полного QA: прежний detached dispatcher обрабатывал независимые self/product reviewer последовательно; один задержанный self-review блокировал завершение другого. События `run_completed` Stop также фиксировались по очереди. В собственном Go-исходнике изменены только два файла и добавлены два тестовых.

- Stop теперь сохраняет `run_completed` независимо для self/target параллельно, **дожидается обоих** перед запуском reviewer; ошибки и `run_id` сохраняются.
- Один detached child обрабатывает разных владельцев параллельно, внутри одного владельца — строго последовательно; максимум **2 одновременно исполняемых reviewer**. JSON-квитанции остаются в исходном порядке, ошибки не скрываются.
- TDD RED на 3 одновременно исполняемых reviewer — RC1; ограничение до двух — GREEN **20 повторов RC0**.
- Реальный Windows Stop/reviewer — **5/5 PASS RC0**, длительности от 1,406 до 2,024 с при прежнем лимите 5 с.
- Окончательный `go test -count=1 -timeout 15m ./...`: **RC0**, пакет Go 462,655 с; tray RC0. `go vet` и `go build` **RC0**.
- Независимый read-only Codex **gpt-6-sol medium** на [точном исходном коммите](https://github.com/ruhorh66-rgb/air-plugins/commit/ddb09dd942a873d6b51052dc7f60a773091bc22e): **PASS, P0/P1/P2=0**. Судья прочёл diff и смежную реализацию, но не выполнял host QA сам.

**Машинная квитанция:** [JSON](receipts/AIRWORKER_N124_A3_STOP_BOUNDED_FULL_QA_ACCEPTANCE_20261019.json), включает SHA-256 логов, точные коды выхода, SHA судьи, исходный и текущий GitHub HEAD и критерии rollback. Локальные тестовые журналы хранятся под `R:\-4-\air-worker\work\N124_WORKER_A_20261019`.

**Граница эксплуатации:** всё это — исходники, а не установленный релиз. AirWorker 0.11.8 с SHA `1b4008af…4a94a94` остался неизменён. **N-124 OPEN**: реальная trusted GitHub authority и долговечный generation CAS ещё не приняты. DEV-136 `-apply`, AirLegal N-111 и публикация/установка 0.11.9 не разрешаются этим source PASS. Чужие WIP, `PLAN.md`, `plan/N-*`, LEARN и production claim refs не менялись.

**Следующая веха:** N-124 GitHub durable generation/unique numeric N-ID с одним доверенным publisher, отрицательными тестами и отдельной production-квитанцией. Урок о сериализации двух независимых reviewer зафиксирован кандидатом в JSON; в LEARN штатным ядром — только после принятой N124 authority. Никакого вызова Voody для исправления этого дефекта не потребовалось.
