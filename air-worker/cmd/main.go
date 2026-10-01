// Command air-worker — механизм продукта одним исполняемым файлом.
//
// ИМЯ ФАЙЛА — ИМЯ ПРОДУКТА. Поправлено ЛПР 13.09.2026 в тот же день: первая сборка
// называлась woody.exe по имени механизма, хотя механизм-скил в этот же день был
// расформирован и влит сюда. Имя расформированного продукта, воскресшее в выпускаемом
// файле, — это не мелочь оформления: по имени артефакта человек судит, чем он владеет.
// Дятел Вуди остаётся названием ПЕТЛИ внутри продукта, а выпускается air-worker.
//
// ЗАЧЕМ БИНАРНИК. Решение ЛПР 13.09.2026, и довод его же: собранный файл никуда не
// ставится, лежит рядом с продуктом и НЕ ТРЕБУЕТ ПОВЫШЕНИЯ ПРАВ. Это снимает тот самый
// класс гейтов, на котором сегодня встали две соседние площадки.
//
// Побочно он закрывает беду, которая за двое суток стоила нам шести поломок и ни одна
// из них не кричала: КОДИРОВКИ. В PowerShell их четыре независимых места — запись файла
// (лечится BOM), перенаправленный вывод дочернего процесса (преамбулой в нём самом),
// прямой вывод в консоль (присвоением OutputEncoding) и stdout интерпретатора. Починка
// одного не покрывает остальные, и мы наступали на это по очереди. В Go строка — UTF-8
// по определению, и вопрос перестаёт существовать, а не решается аккуратностью.
//
// ЧТО ПЕРЕНЕСЕНО И ПОЧЕМУ ИМЕННО ЭТО. Судья и двигатель цели: они зовутся чаще всего
// (каждый ход стража и каждая итерация петли), они детерминированы, и их поведение
// закреплено прогонами — то есть равенство со старой реализацией можно ДОКАЗАТЬ, а не
// объявить. Петля и планировщик остаются шагами плана: петля управляет внешними
// процессами и её перенос менять поведение не должен, а планировщик тратит деньги, и
// переносить его без доказанного равенства дешёвых частей значило бы рисковать дорогим.
//
// СОВМЕСТИМОСТЬ — ОБЯЗАТЕЛЬСТВО, А НЕ ПОЖЕЛАНИЕ. Коды возврата, текст вердикта и формат
// файлов совпадают со скриптами до символа. Иначе переход пришлось бы делать «большим
// прыжком», а так обе реализации живут рядом, и расхождение видно сверкой.
package main

import (
	"fmt"
	"os"
)

// version печатается по подкоманде и пишется в машинный вердикт: по нему видно, чем
// именно посчитан результат. Без этого две реализации рядом неразличимы в журнале.
const (
	appName = "air-worker"
	version = "0.11.5"
)

func usage() {
	fmt.Fprint(os.Stderr, `air-worker — механизм гарантированного результата

  air-worker judge  -product <корень> [-config <путь>] [-min-facts N] [-json]
        Судья цели. Коды: 0 достигнута, 1 не достигнута, 2 нечем проверить.
        -json отдаёт тот же factual run структурой: checks/criteria/state/reason/duration/selector.

  air-worker drift  -product <корень> [-record] [-note "..."] [-json] [-quiet]
        Двигатель цели. Коды: 0 ALLOW, 1 THROTTLE, 2 ESCALATE, 3 ЖДЁТ ЛПР.
  air-worker drift -all [-registry <air-worker.products/v1.json>] [-record] [-history <path>] [-json]
        Portfolio drift по effective-distance каждого зарегистрированного продукта.
        Сравнивает только одинаковый distance_source; смена источника начинает новый baseline.

  air-worker loop   -product <корень> [-config <путь>] [-plan-only] [-whatif] [-orchestrate] [-subagents N]
        Петля: следующий незакрытый шаг плана на назначенной ступени, до вердикта
        либо до объявленного потолка. -orchestrate включает проверяемый native Agent mode.

  air-worker orchestrate -product <корень> [-config <путь>] [-subagents N]
        Штатный chat orchestration entrypoint: эквивалент loop -orchestrate. AirWorker сам
        разрешает Agent tool и принимает успех только при доказанных Agent start/result.

  air-worker executor list -product <корень> [-json]
  air-worker executor dispatch -product <корень> -class bulk|standard|complex|hardest -task-file <путь>
        -principal <p> -session-key <k> [-tier <approved-tier>] [-json]
        GPT-safe вход к утверждённой OpenAI/Codex лестнице. Dispatch требует объявленную
        активную сессию и durable receipt; vendor-limit использует только зарегистрированные fallback.

  air-worker semantic -product <корень> -step <N> [-executor claude|codex|chatgpt|router] [-claim "..."]
        Независимый read-only Semantic Judge текущего шага без запуска executor loop.
        Factual scope ограничен критериями шага; общий verdict продукта показывается отдельно.
  air-worker plan-review -product <корень> [-executor auto|claude|codex|chatgpt|router] [-force] [-json]
        Read-only Semantic Plan Review текущей разбивки. Повторный model-call делается только
        при изменении composition (добавление/удаление/порядок/ступень), не при text/done edit.

  air-worker plan   -product <корень> [-apply] [-model M] [-dry-run] [-use-answer <файл>]
        Планировщик: разбивка цели на шаги ОДНИМ дорогим вызовом. Предлагает в
        PLAN.proposed.md; существующий PLAN.md не трогается никогда.
  air-worker plan node new -product <корень> -title <...> -parent <этап> -owner <окно> -done-when <...>
  air-worker plan node close <id> -product <корень> -receipt <ref>
  air-worker plan node list -product <корень> [-open] [-stale 24h] [-no-owner] [-json]
  air-worker plan spine -product <корень> [-json]
  air-worker plan migrate -product <корень> [-owner <окно>] [-trigger <слово ЛПР>]
        L11-7: PLAN.md остаётся нитью, подробности живут в plan/N-*.md; запись узлов
        выполняется только ядром, закрытый узел остаётся в истории со статусом closed.
        migrate раскладывает существующие ##-разделы дословно, не угадывая owner/status,
        и пишет lifecycle-события узлов в learn/events.jsonl.
  air-worker plan-lint -product <корень> [-json]
        Неблокирующая формальная подсказка: разведочный model-step, уже измеряемый
        существующей проверкой и без mutation/file target, возможно должен быть script.

  air-worker adapter -action status -product <root> [-config <path>]
        Emit compact read-only air-worker.tool/v1 campaign status JSON.

  air-worker tool   [-which claude|codex|opencode|router]
        Каким исполнителем пойдёт петля и каким правилом он найден. Ничего не
        запускает и не стоит ни копейки.

  air-worker encoding -path <файл|каталог> [-fix] [-quiet]
        Правило о BOM одним местом: .ps1 обязан его иметь, .md/.json/.go — не имеют
        права. Зовётся хуком при каждой записи файла, чтобы правило исполнялось,
        а не помнилось.

  air-worker report -product <корень> [-json] [-cached|-no-judge] [-informational|-no-fail]
        Числа хода одним замером. По умолчанию factual judge запускается и обновляет
        fingerprinted verdict. -cached/-no-judge только читает свежий machine verdict;
        stale/missing = NOT_PROVEN. -informational/-no-fail меняет только process exit на 0,
        factual judge_code внутри данных не переписывается.
  air-worker report -all [-registry <air-worker.products/v1.json>] [-json]
        Сводка экосистемы без LLM: у каждого продукта явные root+plan, расстояние до
        ближайшей плановой вехи, native goal-distance если доказуем, иначе NOT_PROVEN/limits.

  air-worker validate -product <корень> [-json]
        Механическая проверка PLAN/run-config/judge: файлы, NUL, selector-контракты и
        измеримость критериев. Business/judge checks не запускает; Go selectors перечисляет
        одним go test -list на проверку.
  air-worker goals  -product <корень> [-json]
        Годен ли план к работе: использует тот же validation engine. Коды: 0 годен,
        1 не годен, 2 продукта/плана нет. Решение ЛПР 14.09.2026: без плана и целей
        air-worker не работает; неизмеримый selector не считается годным.

  air-worker feedback -product <root> -source-version <v> -type defect|friction|idea -severity P0|P1|P2|P3
        Capture operational feedback as immutable product evidence plus a non-executable
        candidate in the canonical PLAN. Full success or explicit PARTIAL with nonzero exit.

  air-worker learn add|event|migrate-legacy|propose|pending|apply|effect|context|rollback ...
        Петля самообучения AirCurator. Фоновый разбор может только предложить правило.
        "да <id>" принимается только из доверенного UserPromptSubmit активной сессии и создаёт
        одноразовый grant; apply потребляет grant. Прямая запись в runtime и durable learn блокируется.
        Каждая мутация имеет digest-ledger и обратимый blob.

  air-worker curator patrol|digest -product <root> [-state-dir <dir>] [-json]
        Одноразовый обход/сводка встроенного AirCurator: ближайшая веха, шаг/гейт,
        очередь PENDING_LPR, эффект обучения, peer/assignment state, audit decision count
        и deterministic wake-card из orchestration state.
  air-worker curator tick -product <root> [-now <RFC3339>] [-json]
        L11-7: счётчики нити без модели — открытые узлы, без владельца, без движения >24 ч.
  air-worker curator peer register|list ...
        Durable registry внешних curator peers: provider/model/enabled/max_active.
  air-worker curator assignment assign|list|revoke ...
        Scope product/profile/session/run; один активный curator на scope и лимит peer.
  air-worker curator decision record|list|verify ...
        Append-only tamper-evident audit journal. Запись никогда не является approval/grant.
  air-worker curator wake -product <root> [-dry-run=true] [-json]
        Только расчёт wake-card; transport, SendMessage и запуск resume_cmd не выполняются.
  air-worker curator digest -all [-registry <air-worker.products/v1.json>] [-json]
        Сводка 10:00 SGT по экосистеме: вехи из report -all, ЖДЁТ ДА и эффект обучения.
        Планировщик ОС может звать эту команду; отдельного AirCurator runtime нет.

  air-worker selfcheck [-json] [-live <air-worker>] [-claude-config <dir>] [-codex-config <dir>]
        Read-only distribution identity: live version/revision/SHA, Claude/Codex GitHub
        marketplace cache version/revision/payload snapshot, active config dirs and profile ambiguity.
        Same-version revision/SHA drift is a violation, not "already latest".

  air-worker update status|check|download|install|channel [stable|prerelease]
        Штатное обновление через GitHub: подписанный Ed25519 channel manifest, immutable
        Release assets CLI+tray, SHA-256+size, запрет downgrade, синхронизация известных
        Claude/Codex plugin caches штатными командами, транзакционная замена и rollback.
        check -if-stale использует интервал ядра и не создаёт второй таймер в трее.
        install никогда не выполняется фоном: запуск — только явным действием пользователя.
  air-worker update payload -root <plugin-root> [-json]
  air-worker update verify -manifest <file> [-asset-dir <dir>] [-channel stable|prerelease] [-json]
        Release-only/read-only проверки тем же кодом клиента: canonical payload SHA и
        подпись/URL/hash/size манифеста перед продвижением channel feed.

  air-worker install [-dir <куда>] [-autostart] [-no-start] [-status] [-uninstall]
        Ставит продукт в пользовательскую область и вешает значок в трее. ПОВЫШЕНИЕ ПРАВ
        НЕ ТРЕБУЕТСЯ ни на одном шаге: файлы идут в %LOCALAPPDATA%\air-worker. Значок
        поднимается ЭТОЙ ЖЕ командой и живёт до выхода; автозапуск при входе в систему
        по умолчанию НЕ объявляется — это решение владельца машины, а не установщика,
        и берётся флагом -autostart. -status отвечает разными фактами — файлы, PATH,
        автозапуск, живой значок, — а не одним «установлено».

  air-worker tray -ensure | -stop | -status [-quiet]
        Поднимает значок, если он ещё не поднят. ИДЕМПОТЕНТНО: на машине работают
        несколько сессий, каждая зовёт это при загрузке плагина, значок остаётся ОДИН —
        гонку разрешает именованный мьютекс внутри самого значка. Зовётся хуком
        SessionStart, поэтому при -ensure молчит, когда делать нечего.

  ОТКАЗАТЬСЯ ОТ ЗНАЧКА: переменная AIR_WORKER_NO_TRAY (любое значение, кроме пустого
  и «0»). Читается из окружения процесса и из ветви пользователя. Значок — вещь машины,
  а не продукта, поэтому выключатель в окружении, а не в run-config.json: конфигурация
  одного продукта не должна решать за второй на той же машине.

  ВЫВОД В ПЕРЕНАПРАВЛЕННЫЙ ПОТОК — UTF-8 БЕЗ МЕТКИ. Кодовая страница консоли на
  перенаправление не действует, и читающая сторона обязана объявить кодировку сама:
  в PowerShell [Console]::OutputEncoding = [Text.Encoding]::UTF8 перед вызовом. Метку
  порядка байт продукт НЕ СТАВИТ намеренно — она сломала бы разбор -json у стража и
  у значка.

  air-worker session declare -product <корень> -principal <p> -session-key <k>
        Host-neutral identity сессии: режим, продукт и гейт ЛПР пишутся и читаются ТОЛЬКО
        в namespace principal+session-key, объявленном этой командой — не угадываются из
        переменных окружения конкретного харнесса (например CLAUDE_CODE_SESSION_ID).
        Адаптеры (Claude/Codex/GPT) передают сюда свою identity, а не создают её сами.
  air-worker session status -principal <p> -session-key <k>
        Режим, продукт и (если выключена) слова ЛПР дословно — той же сессии.
  air-worker session off  -principal <p> -session-key <k> -words "<слова ЛПР>"
        Выход сессии из работы словом ЛПР, дословно (≥6 знаков). Снимает продукт сессии.
  air-worker session on   -principal <p> -session-key <k> -words "<слова ЛПР>"
        Возврат сессии в работу тем же правилом; продукт объявляется заново.

  air-worker hook <событие>
        Host-neutral lifecycle/control entrypoint: событие читается JSON со stdin.
        Поле principal опционально: отсутствие означает claude для совместимости;
        GPT/Codex adapters передают свой principal явно. Решение целиком принимает бинарник.
        До включения Air Worker для сессии — no-op;
        telemetry/lifecycle (Stop, SubagentStart/Stop, SessionStart, PostToolUse,
        UserPromptSubmit) при активной сессии — fail-open с записью в след; control
        (PreToolUse) при активной сессии — fail-closed с причиной в stderr, код 2;
        неизвестное событие — fail-open с записью в след. Паника обработчика отвечает
        по классу события, а не кодом 2 голой паники.

  air-worker version
`)
}

// main — ОДНА точка выхода: os.Exit(run(...)). Раньше os.Exit стоял в каждой ветви
// switch, и код подкоманды доходил до процесса только пока никто этого не трогал.
// AIR-ENV-002 14.09.2026: отказ установки (`return 2` в ветке «рядом нет
// air-worker-tray.exe») дошёл до процесса нулём — код терялся в обёртке снаружи Go.
// Здесь код возврата собран в один шов run() int, который проверяется тестом
// прогоном самого процесса: подкоманда возвращает число, main его отдаёт, и никакая
// ветвь больше не может завершить процесс молча нулём.
func main() {
	setConsoleUTF8()
	os.Exit(run(os.Args[1:]))
}

// run — разбор подкоманды в код возврата, БЕЗ os.Exit. Отделён от main ровно ради кода
// возврата: так его можно проверить и вызовом функции, и прогоном процесса, не убивая
// тестовый процесс через os.Exit.
func run(argv []string) int {
	if len(argv) < 1 {
		usage()
		return 2
	}
	switch argv[0] {
	case "judge":
		return cmdJudge(argv[1:])
	case "drift":
		return cmdDrift(argv[1:])
	case "loop":
		return cmdLoop(argv[1:])
	case "orchestrate":
		return cmdOrchestrate(argv[1:])
	case "executor":
		return cmdExecutor(argv[1:])
	case "semantic", "review":
		return cmdSemantic(argv[1:])
	case "plan-review":
		return cmdPlanReview(argv[1:])
	case "plan":
		return cmdPlan(argv[1:])
	case "plan-lint":
		return cmdPlanLint(argv[1:])
	case "adapter":
		return cmdAdapter(argv[1:])
	case "tool":
		return cmdTool(argv[1:])
	case "encoding":
		return cmdEncoding(argv[1:])
	case "report":
		return cmdReport(argv[1:])
	case "validate":
		return cmdValidate(argv[1:])
	case "goals":
		return cmdGoals(argv[1:])
	case "feedback":
		return cmdFeedback(argv[1:])
	case "learn":
		return cmdLearn(argv[1:])
	case "curator":
		return cmdCurator(argv[1:])
	case "selfcheck":
		return cmdSelfcheck(argv[1:])
	case "update":
		return cmdUpdate(argv[1:])
	case "install":
		return cmdInstall(argv[1:])
	case "tray":
		return cmdTray(argv[1:])
	case "session":
		return cmdSession(argv[1:])
	case "hook":
		return cmdHook(argv[1:])
	case "version", "-v", "--version":
		fmt.Printf("%s %s\n", appName, version)
		return 0
	default:
		usage()
		return 2
	}
}
