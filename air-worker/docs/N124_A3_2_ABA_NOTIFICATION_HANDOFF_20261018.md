# AirWorker N-124 · A3-2 Windows ABA notifications · 18.10.2026 SGT

**Статус: SOURCE_ONLY_SCOPED_JUDGED_PASS / N124 OPEN / production authority HOLD.**

## Фактическая причина и исправление

- Owner: AirWorker line A, curator Voody. Триггер: предыдущий Windows hold запрещал запись в существующие PLAN/N/LEARN-файлы, но пропускал кратковременное создание и удаление нового файла (ABA) между проверками.
- Source branch `dev/airworker-n124-a3-aba-notify-20261018`, база `e658370b803b58f5c8173b49a5e6dea6003f1e8c`. Код: `aa05933b914ed813d8b7f8e76aace8e530e015be`; test cleanup P2: `ce1ec3f1fe221995c7bf366e81e2795f16165b48`.
- Затронуты строго 4 Windows source/test-файла: `plan_canonical_projection_hold_windows.go`, `plan_canonical_projection_hold_windows_test.go`, новый `plan_canonical_projection_notify_windows.go`, новый `plan_canonical_projection_aba_windows_test.go`.
- WinAPI `FindFirstChangeNotificationW` ставит watch на pinned native-каталоги **до** их перечисления; фильтр только имен `FILE_NOTIFY_CHANGE_FILE_NAME | DIR_NAME`, включая подпапки. Сигнал остаётся до закрытия, сброс `FindNextChangeNotification` не производится. `Verify` проверяет сигнал до и после чтения PLAN/N/LEARN, при событии — fail closed.
- Исходная версия broad-notification маски давала ложные срабатывания при baseline read и была отклонена. Новая names-only маска проходит положительные и отрицательные тесты.
- Два процесса: отдельный Windows test-process создал и удалил N-файл во время удержания; оба последовательных `Verify` обнаружили событие. Новая чистая фикстура после Close снова допускает новый независимый hold.

## Host QA и независимые судьи

| Этап | Проверенный факт | SHA256 лога/вердикта |
|---|---|---|
| Настоящий TDD RED | 3 ABA FAIL, RC1 | `ebd08af437a59f272428f74c153fdb1ef2c91cf4e35c71b0d8168ecd08cdba34` |
| Ложное раннее GREEN | baseline false-positive RC1, маска расширений отброшена | `6f169f7f4ca2141d290cc3c925e56e693ebb80a8b533a98645609dac1b1031b5` |
| Итоговый Windows host QA | 9 основных + 9 вложенных PASS, 0 SKIP, RC0 | `2dd0230953f276c2192057470b9181e71455a5d798bb9f2d4ce2697fbd6b09b2` |
| Native new/close/replay + atomic CAS | 8 основных + 14 вложенных PASS, RC0 | `f7a21ecbd9b3d43281dbff4ebcae27b3fc68934f76defb09ad2bdae61f2b36ca` |
| ABA stress ×3 | 6 основных + 9 вложенных PASS, RC0 | `cedf785db599c75a1c392979df21c7d5672dd013b18eff6ba5d6660cfe11aa5a` |
| Windows P2 cleanup correction | 9 основных + 9 вложенных PASS, RC0 | `8c85c9c58ae45f931b189a70989a172d9ea247697f51033f65cdf4f28add814a` |
| Go vet/build на окончательном SHA | RC0 / RC0, isolated EXE на R:, не установлен | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |

- Независимый Codex `gpt-6-sol:medium` на `aa05933`: PASS, P0/P1=0, P2=1 — опасность незакрытого последнего test-hold при `t.Fatal` после переназначения. Вердикт SHA256 `3cc9b4947d6fd7817ad41bc1a810ec6b153f3e63a855128ea9f6910625dbd053`.
- На `ce1ec3f` test-only fix `defer func(){hold.Close()}()` закрывает **текущий** hold. Повторный независимый Sol medium: PASS, P0/P1/P2=0, RC0, вердикт SHA256 `af585224f02b58acbef4142fc7f54b287a281c551cf8f987fcff25429f2d55c6`.
- Судья читал frozen source, но самостоятельно не воспроизводил host Go-тесты. Полная Go-регрессия ещё NOT_PROVEN.

## Строгие границы и следующий узел

Этот компонент — **только disposable local Windows OS-temp / NTFS-ReFS fixture**. Он обнаруживает изменения от момента постановки watcher до Close, но не даёт durable GitHub generation CAS до arm/после Close, не утверждает signer/actor и не подключён к production `plan node new/close`.

Проверенный GitHub A3-1 atomic custom refs уже PASS и тестовые ссылки очищены, но **каноническая ветка PLAN/ref и trusted publisher identity не утверждены ЛПР**. N-124 остаётся OPEN; DEV-136 header `-apply` и AirLegal N-111 заблокированы. Никаких ручных правок `PLAN.md`, `plan/N-*` или `learn/events.jsonl`, релизов/установок, UAC/reboot, вмешательства в Hermes WIP или ветку B.

**Квитанция:** [машинный JSON, включая owner/trigger/done_when/judge/rollback и SHA](receipts/AIRWORKER_N124_A3_2_ABA_NOTIFICATION_SOURCE_ONLY_20261018.json). Все первичные host log находятся в `R:/-4-/air-worker/work/N124_WORKER_A_20261018`.

**Откат:** revert только собственные source-only коммиты `aa05933` и `ce1ec3f` в их feature-ветке; установленная AirWorker `0.11.8` SHA256 `1b4008af05110214934d46c0ab8c5236341cd1c699f54efb895c2f9f64a94a94` неизменна.

**Следующая измеримая веха:** один утверждённый canonical GitHub PLAN ref + доверенный publisher/signer + installed binary attestation, затем durable generation CAS с native фактами, полной регрессией и независимым Sol; только после PASS допустимо native close N124 → DEV136 → AirLegal N111.

Windows docs: [FindFirstChangeNotificationW](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-findfirstchangenotificationw) и [FindCloseChangeNotification](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-findclosechangenotification).
