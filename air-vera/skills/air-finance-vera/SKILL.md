---
name: air-finance-vera
description: Finance workflow for budget and accounting documents through the exact accepted native AirVera owner CLI. No automatic AIR Comms, direct-provider, or second-transport fallback when Vera is unavailable or fails; preserve receipts, verify effects, and fail closed on ambiguous mutations.
---

# Air Finance via Vera

Use this skill for budgets, BDR/BDDS, payment calendars, accountant comments, finance workbooks, Yandex publication, and accounting follow-up mail.

## Source of truth and delivery

- Historical canonical skill coordinate (foreign WIP is read-only until N-124 authority selection):
  `F:\-5-\011_Plugins\AirVera_Wiki\air-vera\skills\air-finance-vera\SKILL.md`.
- An accepted Vera bundle under `R:\-4-\air-vera\product\versions\<version>\` is immutable release payload.
- Do not hot-edit the skill inside an accepted bundle. Update the canonical source and ship it through the next accepted release/install.
- Before native Vera work, resolve the current accepted version mechanically; never hard-code an older version path.

## Phase-transition learning

At an explicit phase change, use a **verified available native AirVera learning/checkpoint command** when it exists in the exact accepted binary. Otherwise record the reusable lesson as a development-source proposal/receipt with `NOT_AVAILABLE_IN_ACCEPTED_RUNTIME`; do not invent an installed checkpoint command and do not block urgent finance work on speculative learning.

For finance work, explicitly review:
- what financial artifact/version was accepted or superseded;
- what provider/mail/calendar effect was actually verified;
- what accountant/LPR correction changed the working method;
- what arithmetic/routing rule belongs in `air-finance-vera` or `Air Budget`;
- what one-off case fact belongs only in runtime/Operations Mail;
- what defect needs a Vera/Air Finance Audit backlog item;
- what ambiguous mutation must remain no-retry across sessions.

A new phase is not considered fully initialized until reusable lessons from the previous phase are either persisted or explicitly classified as “no durable lesson”.

## Fast-path execution rule

For a simple operational request, optimize for closing the user's business action first.

1. Identify the **minimum verified outcome** the user asked for.
2. Use already-known owner paths, dataset IDs/aliases, runtime paths and accepted contracts from the current session; do not rediscover infrastructure that is already proven.
3. Do not put research, architecture review, release engineering or deep skill maintenance on the critical path unless the business action actually depends on them.
4. For mail triage/research leads, the fast path is normally:
   - read exact message;
   - classify action;
   - create/update one Operations Mail card;
   - schedule a bounded research block if needed;
   - stop.
5. Defer deep vendor research to the scheduled research block. A quick classification may use only the message and already-known facts.
6. Phase learning must be **bounded and non-blocking**: capture reusable rules after the operational action, unless a safety/idempotency rule must be known before mutation.
7. Prefer one owner-tool call that closes a step over multiple diagnostic calls that only reconfirm already-proven state.
8. If an operation is taking materially longer than expected, ask: “Am I proving something already known, or is this evidence required to safely complete the user's request?” Drop non-required checks.

Target: simple mail/calendar/card operations should normally complete in a few direct owner-tool calls, with learning recorded afterward.

## Routing rule — one owner, fail closed

**Default invariant:** finance work uses only the current accepted native AirVera owner surface for Vera-owned mail/files/calendar/contacts. An unavailable or failed Vera path is `BLOCKED`, not permission to switch to AIR Comms Cloud, rclone, legacy MCP, or a direct provider. A separately authorized new workflow outside Vera is not a fallback or a retry of an uncertain Vera effect. No skill can grant authority beyond native CLI/LPR gates.

1. Before a provider operation:
   - resolve the current accepted AirVera version and exact executable;
   - verify `health --json` and resident tray identity;
   - use `air-vera files` for Yandex/WebDAV and `air-vera mail` for mail.

2. Yandex Disk / WebDAV:
   - use native Vera `files list/stat/get/put/update/...` first, independent of where the task was initiated;
   - for availability/integrity checks, prefer `list` + `stat`; use read-only `get` to a temporary runtime file when exact size/SHA-256 proof is needed, then delete the temporary file;
   - do not choose AIR Comms simply because it is easier or already exposed in the ChatGPT host.

3. Source exists only in ChatGPT/OpenAI Library:
   - keep Vera as the owner/orchestrator;
   - use a supported transport to deliver the same bytes to Vera, verifying filename, size and SHA-256;
   - if no supported transport into Vera exists, report `BLOCKED / source_transport_unavailable`, preserve original bytes and stop dependent steps; do not silently upload via AIR Comms or any other non-owner provider.

4. Source exists physically on SRVLM01 or another AIR node:
   - use native Vera directly;
   - do not bounce the file through OpenAI Library or AIR Comms merely for convenience.

5. Mail:
   - use native `air-vera mail` exclusively for Vera-owned mail;
   - IMAP and SMTP use the same configured account credential unless the LPR explicitly changes that contract;
   - if Vera is unavailable before the attempt, stop the mail operation; do not silently change transport;
   - after an ambiguous Vera mutation, never switch transports or repeat the action: reconcile the original Vera effect first;
   - report a business message as SENT only after provider/Sent/readback evidence supports that exact message.

6. Vera-unavailable blocker must be factual, for example:
   - SRVLM01/AIR Commander is unreachable;
   - accepted Vera runtime or resident owner surface cannot be reached;
   - the exact required Vera operation/transport is not available in the current tool path.
   Record the reason, receipts and incomplete effects; do not mark a provider operation complete and do not change owner transport. Resume by Vera after recovery.

## Public native approval / version boundary

For every operation verify effective accepted AirVera version, executable, resident health, installed CLI help, account, exact payload and permission. Public native verbs never allow handcrafted protocol-grant or AdapterEnvelope.

- **Installed 0.2.7+ files approve (verify actual 0.2.8 CLI):** request a one-use LPR grant bound to account/href/input SHA using: air-vera files approve --account ID --href /path --input FILE --authority-file LPR_KEY --grantor EXPLICIT_LPR --output NEW_GRANT.json --json. For an existing file require --action update --etag CURRENT_ETAG. Then one matching files put/update --grant; provider readback independently verifies exact bytes. Approve itself does not upload. Never use another transport if unsupported.
- **Installed 0.2.7+ mail confirm (verify actual CLI):** IMAP copy into Sent and move/archive use distinct operator confirmation, exact account/source folder/UID/destination, followed by provider Message-ID readback. Mail approve is for higher LPR mail.send authority, NOT a mail placement grant. Do not treat a separate reply as proof of Sent-copy.
- **Native mail approve:** air-vera authority status --json identifies a valid LPR authority location. Only after final signature/attachment/preflight and verified compose bind exact payload, target and grant using the public air-vera mail approve command. A new payload requires a new target/grant; approval has no provider effect.
- **Not installed:** in 0.2.9 source code, public calendar approve for caldav.create and contacts approve for carddav.create/update/merge include collection/target/strong-ETag read-only preflight, but neither is available in installed 0.2.8. Contact delete is separate LPR. Never claim release or live provider compatibility from source-only tests.

## Resolve the active Vera runtime

Before any native mail/files operation:

1. Determine the actual **effective accepted version** from the installed native Vera `version --json` and `release-status --json` readback, checking resident executable identity. In 0.2.8+ with a locked legacy pointer, the authoritative accepted state can be the verified append-only accepted-log, while the original `R:\-4-\air-vera\product\accepted.json` intentionally stays at the older immutable baseline. Do NOT treat the legacy file alone as current or edit it manually.
2. Confirm:
   - `accepted_version`;
   - `bundle_root`;
   - `revision`;
   - `status=PASS`.
3. Run `health --json` and confirm resident tray identity points to the same accepted executable.
4. Run `package-health --json` when release/package integrity matters.
5. If `air-vera` is absent from PATH, invoke the exact accepted `air-vera.exe` by absolute path. PATH absence is not evidence that Vera is uninstalled.

Never use a stale hard-coded version directory after the **effective native accepted version** advances. A 0.2.8+ accepted journal is resolved by the native CLI; raw accepted.json is a legacy anchor and may show an older version. Corrupt journal/anchor identity is BLOCKED, never silently a fallback.

## Owner failure and host diagnostics (no provider fallback)

When AirVera cannot execute a Vera-owned provider operation, report `BLOCKED`, recording accepted version, `health/status`, exact native verb and receipt/failure, without using a second provider transport. An `UNKNOWN` effect retains its original idempotency identity and requires native read-only reconciliation before any further mutation. Do not invent an authority grant, trigger manual provider writes or repeat the action through AIR Comms/rclone/legacy mail MCP.

AIR Comms host-cache diagnostics (`comms_help.runtime_version`, `published_tools`, `published_tool_count` compared with actual host tools) may be performed **only as a separate diagnostic task**; `TOOL_CACHE_STALE` does not authorize execution of the blocked finance mail/files request through AIR Comms. Return to Vera native CLI after recovery.

## Yandex Disk budget workflow

Default finance destination when instructed:
`/001 Бюджет РСУ/Бюджет АИ/`

Normal Vera path:
1. Resolve the active accepted Vera binary and confirm health/resident identity.
2. Use `air-vera files list` on the destination folder.
3. Use `air-vera files stat` on the exact versioned workbook.
4. For an availability/integrity check, use read-only `air-vera files get` into a temporary Vera runtime file when exact byte size/SHA-256 proof is required.
5. Compare filename, size, ETag/provider identity and SHA-256 to the expected version.
6. Remove temporary readback files after verification.
7. For a new upload/update, preserve earlier budget versions unless LPR explicitly requests replacement and verify the provider effect after the mutation.

If the Vera owner path is unavailable, leave publication unperformed and report the blocker. Historical AIR Comms transfers do not authorize a new non-owner upload.

## Mail mutation contract

### Обязательная корпоративная подпись исходящих писем (уточнение ЛПР 08.10.2026)

**Обязательная отметка об ИИ (прямое уточнение ЛПР 08.10):** каждое исходящее деловое письмо, которое подготавливает данный ИИ-агент, должно содержать в конце письма точную строку **«Письмо подготовлено с помощью ИИ-агента.»**. Это часть подписного блока, а не техническая пометка в черновике/квитанции. Проверять её присутствие и точную UTF-8 запись до native `mail.compose` и повторно в фактически отправляемом `mail.send` payload; без неё операция BLOCKED. Не заменять отметку только строками ФИО, должности и организации.

**HARD PREFLIGHT GATE:** до любого `mail compose` / `mail send` проверить подпись по принятому профилю отправителя или доказанной предыдущей деловой переписке. В `text`/`html` передавать **полный подписной блок**: имя/ФИО, действующая должность, организация и проверенные контакты (если входят в утверждённую подпись). Одинокое «С уважением» или одно только ФИО не удовлетворяют этому гейту. Адрес отправителя из `accounts` не является готовой подписью. Если точной подписи нет — спросить ЛПР, не выдумывать должность, телефон, реквизиты или юридическое лицо.

Для отправки через native Vera подтвердить `SIGNATURE_VERIFIED` и `BODY_UTF8_ROUNDTRIP` **до** необратимой SMTP-мутации вместе с sender, To, необходимой контрольной self-CC, темой и каждым вложением по SHA-256. При исправлении подписи старый payload/target/grant считается устаревшим: готовить новый payload + отдельный target, выдавать новый grant только по принятой authority policy. Никогда не отправлять старый подписанный/неподписанный черновик по инерции. `mail.compose PASS` — не `mail.send PASS`. Если host/tool блокирует отправку, фиксировать `NOT_SENT / HOST_TOOL_BLOCKED`, не обходить его альтернативной почтой и не объявлять delivery.

**Обучение:** после каждого пропуска подписи записать случай и превентивную проверку в receipt; постоянные правила — в этом source-skill, точные имена/реквизиты конкретного ящика — в runtime/account-specific record, не в общем product Git. Installed bundle менять только через утверждённый релиз.


Before a mail mutation:
1. Confirm the intended account and exact recipient.
2. Use a unique target/idempotency identity for the intended message.
3. Compose first and verify the composed payload identity where supported.
4. Run `air-vera authority status --json` and use the reported valid LPR authority path; never copy key contents into chat or payloads.
5. Issue a fresh exact grant with public `air-vera mail approve --account ... --folder ... --target ... --payload ... --authority-file ... --grantor ... --output ... --json`. The command must bind to the already completed/verified compose receipt and has no provider effect.
6. Execute `mail send` exactly once with the saved `--grant`. Never call internal `protocol-grant` or hand-build AdapterEnvelope files in normal exploitation.

After the send attempt:
- PASS + matched provider/Sent readback => VERIFIED SENT.
- Definitive failure with proven no effect => failed; a controlled retry may be considered under policy.
- `RECONCILE_REQUIRED`, receipt `state=unknown`, timeout, or ambiguous provider outcome => UNKNOWN. Do not retry.
- For UNKNOWN, inspect the original native receipt and exact provider state only through read-only commands supported by the installed accepted Vera CLI. If no authorized native read-only reconciliation/readback path exists, retain `UNKNOWN/BLOCKED`; never retry or change provider transport.

## SMTP self-canary diagnostic

Use a self-canary only as a diagnostic when the SMTP/send/reconcile path itself is in doubt; do not add a canary to every normal finance send.

Procedure:
1. Use the current accepted Vera binary and healthy resident tray.
2. Send from the configured account to the same account.
3. Generate a unique subject/target so readback cannot collide with older mail.
4. Compose and record the exact payload digest/message identity.
5. Issue a new exact approval grant.
6. Perform one `mail send` attempt only.
7. If send becomes UNKNOWN:
   - do not send again, construct an `AdapterEnvelope`, or invoke internal `protocol-reconcile` in normal operation;
   - inspect the original native send receipt and, only where supported by the installed accepted Vera CLI, use native read-only mail list/read or a documented native read-only reconciliation operation;
   - match subject, sender, recipient, body/message-id and exact idempotency identity where available;
   - if the approved native read-only path or exact provider evidence is unavailable, keep `UNKNOWN/BLOCKED` and stop without another mutation.
8. Record receipt id, idempotency key, provider UID(s), message-id, and evidence path in backlog/receipt.

Interpretation:
- Exact self-delivery proves the SMTP mutation took effect even if the control plane returned UNKNOWN.
- Self-delivery does not by itself prove that Vera's post-send verification or Sent-copy handling is healthy.
- If self-delivery succeeds while reconcile fails, classify the defect as post-send effect verification/reconciliation (and possibly Sent-copy handling) until deeper evidence proves another cause.
- Do not diagnose a separate SMTP credential failure when the same configured credential is healthy for account access and the canary actually arrives.

## Business-message verification rule

For a real recipient, a successful self-canary is only transport diagnostics; it does not prove the real business message was sent.

For the exact business message:
- prefer verified Sent/provider receipt/readback tied to that exact payload/message identity;
- if the original send result is UNKNOWN and exact provider evidence is absent, report `NOT VERIFIED SENT`;
- do not repeat the real message merely to obtain certainty;
- first reconcile/read back provider state and preserve the original idempotency/evidence chain.

## Continuous mail-triage loop

When processing an inbox sequentially, closing the current message is **not** a stopping point.

Default loop:
1. perform the approved action for the current message;
2. verify the mutation/readback when required;
3. immediately fetch and open the next newest message;
4. present the next message in the standard mail-context format;
5. stop only when:
   - LPR explicitly says to stop/pause;
   - the inbox is empty;
   - a new message requires a decision/rule from LPR;
   - a safety/authority blocker prevents continuing.

Do not end the turn merely with “current message closed” when the user's standing intent is to continue through the mailbox.

## Mail context reporting

When presenting a newly opened business message to LPR, do not over-compress the report. Give enough context for LPR to supervise the decision without reopening the message.

Default report should include, when available:
- mailbox/folder and UID;
- sender name + exact email/domain;
- recipient(s);
- exact subject;
- message header send date/time and timezone;
- provider/mailbox delivery or internal-received time **only if actually exposed by Vera/provider**;
- if exact delivery time is unavailable, say so explicitly and optionally state only the proven latest fact, e.g. “present in INBOX by <read/search time>”; never label read time as delivery time;
- attachment count, filenames, types, sizes and SHA-256 when relevant;
- concise but sufficiently complete body/context summary;
- what the sender is asking the recipient to do;
- any stated deadline/effective date;
- cited law/regulation/contract reference exactly as written in the message;
- operational classification and recommended next action.

Expand uncommon abbreviations on first use where practical (for example, electronic document interchange, electronic transport waybill, qualified electronic signature, machine-readable power of attorney) instead of assuming the LPR wants an abbreviated summary.

Clearly separate:
1. facts stated in the message;
2. facts verified elsewhere;
3. assistant inference/recommendation.

## Research-value mail triage

Do not automatically archive a message merely because it is promotional, newsletter-like or no-reply.

Before archiving, ask whether it contains a potentially useful:
- vendor/product capability;
- new technology or integration pattern;
- market/competitor signal;
- tool that may be relevant to AirOS/AirVera/Air Finance workflows.

If yes:
1. classify it as a research lead rather than routine advertising;
2. create an Operations Mail card through the installed **AirWiki public CLI/JSON writer**; AirStorage may index an accepted card separately, but is not the canonical Wiki writer;
3. capture the reason for interest and bounded research questions;
4. if the research deserves dedicated attention, schedule a calendar block instead of doing an unbounded deep dive inline;
5. keep the source message available until the research case is either completed or explicitly closed, unless LPR says to archive it immediately.

If no durable relevance exists, ordinary archive/no-reply handling is appropriate.

## Accountant incoming mail

When reading finance/accounting mail through Vera:
1. Read the exact newest matching message first.
2. If the message body is empty but attachments exist, treat attachments as the likely substantive content; do not conclude that the message is empty.
3. Use `mail attachments` and `mail attachment-download` read-only through Vera.
4. Verify attachment filename, size and SHA-256.
5. For small text attachments, inspect raw bytes/encoding when decoded output is garbled. Legacy Windows-1251 accounting TXT files are possible; `????` in console output is not proof that the source file is corrupted.
6. Remove temporary downloaded attachment files after reading.
7. Preserve the accountant's exact cell references/formulas before interpreting or editing the workbook.

## Accountant message closure workflow

When an accountant remark causes a budget/model correction, close the communication in this order:

1. Resolve the substantive finance issue and produce a **new version** of the workbook by default; do not overwrite the previously accepted version unless LPR explicitly requests replacement.
2. Publish the new workbook through Vera owner-path and require provider readback of exact filename, size and SHA-256 before closing the source mail.
3. Only after the resulting artifact is verified, archive the processed source message through native `air-vera mail archive` with the required confirmation grant and exact Message-ID readback.
4. Create an operation card through the installed AirWiki public CLI/JSON writer in the canonical Operations Mail vault. AirStorage dataset/RAG indexing is a derived read/search layer, not the canonical card writer.
5. The card must preserve at least:
   - original mailbox/UID and Message-ID;
   - sender/subject or attachment identity;
   - attachment SHA-256 where applicable;
   - issue/remark;
   - action/result;
   - published workbook path/version/SHA-256;
   - archive provider reference;
   - reply status and send receipt/idempotency key.
6. Require `verified_by_read`/hash readback from the installed AirWiki writer before treating the canonical card as durable. If AirStorage indexing is separately required, verify its document/chunk readback as an additional derived-index gate.
7. Then reply to the accountant through native Vera with the exact new workbook name/path and a request for re-check.
8. Execute the reply send **once**. If Vera returns `RECONCILE_REQUIRED` / `UNKNOWN`, do not retry or switch to AIR Comms. Inspect the original receipt and verify that exact message through supported native read-only Sent/provider list/read operations; use a documented native read-only reconcile only if confirmed available. Otherwise keep `UNKNOWN/BLOCKED`, with no new mutation.
9. If the exact reply cannot be proven, record `NOT VERIFIED SENT` in the Operations Mail card metadata with the receipt/idempotency key. A later session must reconcile that original attempt before considering another send.

This order prevents archived-but-unprocessed mail, undocumented finance corrections, duplicate replies, and accidental overwrite of an accepted budget.

## Accountant follow-up

After verified cloud upload:
1. Find/read the relevant accountant message or known recipient.
2. Reply or send a short note identifying the exact Yandex folder and filename.
3. Ask to review the cloud file and return comments.
4. Verify the exact sent message.
5. If SMTP/provider result is ambiguous, report `NOT VERIFIED SENT` until exact-message evidence proves otherwise.
6. Never blind-retry an ambiguous accounting message.

## Канонический бюджет: исходники + постатейная расшифровка (уточнение ЛПР 08.10.2026)

Перед обработкой бюджета восстанавливать **точную последнюю версию** через Library/принятый файл или нативный `air-vera files stat/get` с SHA-256. Не принимать более ранний V6/V7 за активный V8 только потому, что он упомянут в историческом примере. Принятый исходный бюджет не перезаписывать: новая версия имеет отдельное имя, дату, реестр входов и машинный контроль.

1. **Исходники видны в самой книге.** Для каждого бухгалтерского Excel включать отдельные читаемые листы с исходными строками, числовыми значениями и формулами либо явный проверяемый без потерь источник, если техническое копирование недоступно. Реестр содержит точное имя, email/UID/Message-ID или provider href, SHA-256, размер, дату и статус принятия. Не выдавать ссылку на отсутствующую книгу за вложенный исходник.
2. **Нет «голых» свёрнутых сумм.** Каждый укрупнённый показатель (хозяйственные, прочие операционные, материалы по объектам, спецтехника, ФОТ и т.п.) имеет отдельную постатейную детализацию с `статья → исходный файл/лист/ячейка → сумма → месяц → БДР/БДДС → подтверждение`. Итог выводится формулой из строк, а сверка с уже принятой суммой показывается отдельно. Ноль/нераспределённый остаток также отражается явно, а не скрывается.
3. **Прогноз — не источник.** Если по будущим месяцам принят run-rate, раскрыть по статьям, что это перенос октябрьской структуры/сценария, а не новая бухгалтерская первичка. Отсутствующая разбивка по объектам показывается как `НЕ РАСПРЕДЕЛЕНО`; не выдумывать доли объектов.
4. **Новое письмо — не автоматическое увеличение расхода.** Разбирать каждую строку, проверять пересечения с текущими исходными статьями, материальными нормативами, СИЗ и резервами; не дублировать «отходы», «охрану труда» или «непредвиденные». Без подтверждённого назначения и периода суммы сохранять в явном `ПОСТУПИЛО / НЕ РАЗНЕСЕНО` и отражать сценарный дельта-контроль, но не менять БДР, БДДС и даты платежей догадкой.
5. **Приёмка.** Проверить наличие исходных листов, постатейных сверок для всех крупных категорий, нулевые обоснованные разницы и отдельно объяснённые сценарные отклонения, сохранность исходных формул/листов, отсутствие новых `#REF!/#DIV/0!`, версионирование и SHA результата. Для публикации в Яндекс.Диск нужен native Vera `files put` с действительным authority grant и независимым readback; наличие локального кандидата не означает опубликованный/принятый бюджет.

## Deterministic budget arithmetic — Air Budget pilot

**Выполняющий числа — скрипт, не LLM.** ЛПР подтвердил 08.10.2026 необходимость этого механизма для любых сумм бюджета, включая постатейные вводные. Семантическая классификация, период начисления, налоговый режим и платежные даты — отдельные authority-вопросы; арифметика и формулы должны генерироваться и сверяться кодом.

- Источник пилота: `AIR Finance Audit / finance_audit/air_budget.py` в выделенном продуктовом worktree; при запуске проверять реальный путь, `python -m finance_audit.air_budget self-check`, версию исходника и тесты.
- Принятый пилот `air-budget plan` пока строит **JSON patch-plan** по налогу на прибыль, а не переписывает XLSX. Не выдавать один план за уже изменённую книгу.
- Для других видов затрат: использовать проверенный расширенный **детерминированный** исполнитель и отдельный case recipe со SHA-256; если такого принятого CLI нет — развивать его в изоляции и выполнять тесты, не считать работу готовой из-за текста скилла. Case-поля, суммы, workbook-cell mappings и бухгалтерские документы живут **в runtime**, не в продуктовом Git.
- При одобрении дополнительных статей расчёт должен отразить их в БДР как отдельные входы, пересчитать связанные итоги и прибыльный налог, сохранить исходные формулы и проконтролировать отсутствие двойного учёта. Если cash-даты не подтверждены, БДДС и платёжный календарь не пополняются воображаемыми платежами: `ACCRUAL_READY / CASH_OPEN`.
- Кандидат без полного независимого пересчёта зависимых формул помечать `RECALC_REQUIRED`, а не `FINAL/PASS`. Публикация через Vera отдельно подтверждается authority grant, re-download и SHA.

For management-budget calculations and spreadsheet formula changes:
- use the AIR Finance Audit `air-budget` pilot before manual LLM arithmetic;
- bind the runtime recipe to the exact source workbook SHA-256;
- keep live workbook cells, current amounts and accountant evidence in runtime/case recipe files, not product Git;
- let `air-budget plan` generate the arithmetic/formula/methodology patch-plan;
- require the same plan to update `00_Методология_AI` whenever model arithmetic changes;
- if the plan returns `BLOCKED`, do not manually fill the unresolved BDDS/payment/tax-calendar mapping;
- only an explicit resolved period/date mapping may unblock cash-layer changes;
- after the pilot is proven across repeated real corrections, migrate its stable operations into AIR Finance Audit core without changing the SHA/evidence/fail-closed contract.

## Finance model controls

Before publishing a finance workbook:
- preserve source facts separately from LPR scenario overrides;
- keep BDR = accrual, BDDS = cash, VAT/tax layer separate;
- retain accountant comments and reconciliation notes;
- do not mark model PASS while unexplained controls remain FAIL/ERROR;
- version every published workbook explicitly.

## Proven Yandex routes — 2026-10-06

**Primary native Vera route:** accepted AirVera 0.2.4 on SRVLM01 successfully verified
`/001 Бюджет РСУ/Бюджет АИ/Бюджет_АИ_2026-10-05_v6_3м_ВКЛ7м.xlsx`
through `air-vera files`:
- `dav.list`: PASS;
- `dav.stat`: PASS;
- `dav.get`: PASS;
- provider ETag/MD5: `55efab94595eeca9207fdb50299f9e77`;
- size: 117884 bytes;
- SHA-256: `cc46734b3875cb7273f89d763532f982b9695b98cac902004722adcec6b448ed`;
- temporary readback file was removed after verification.

This is the preferred operational route.

**Historical comparison only (not operating authority):** an earlier OpenAI-platform -> AIR Comms -> Yandex transfer of the same V6 file had matching size/SHA-256. This receipt does not permit bypassing AirVera or retrying an unknown outcome through AIR Comms.

## Proven SMTP lesson — 2026-10-06

A controlled self-canary on accepted AirVera 0.2.4 produced:
- compose: PASS;
- one send attempt: `RECONCILE_REQUIRED`, receipt state `unknown`;
- `protocol-reconcile`: FAIL at adapter process;
- independent IMAP readback: exact canary present in INBOX with matching subject/body/message-id;
- immediate Sent listing: no matching canary observed.

Operational conclusion for future work: an ambiguous Vera send result can coexist with a real provider effect. Therefore UNKNOWN is a hard no-retry state until provider evidence is reconciled.
