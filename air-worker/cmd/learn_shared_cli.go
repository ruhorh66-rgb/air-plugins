package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ruhorh66-rgb/air-modules/learning"
)

// Match Go flag value consumption before deciding which writer may run. Product
// may appear once only; a duplicate is rejected before either implementation.
func learningProductArg(args []string) (string, error) {
	index := 0
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		index = 1
	}
	product := ""
	seen := false
	for ; index < len(args); index++ {
		token := args[index]
		if token == "--" || token == "-" || !strings.HasPrefix(token, "-") {
			break
		}
		name, value, assigned := strings.Cut(strings.TrimLeft(token, "-"), "=")
		boolean := name == "json" || name == "legacy" || name == "h" || name == "help"
		if !assigned && !boolean {
			index++
			if index >= len(args) {
				return "", fmt.Errorf("flag -%s requires a value", name)
			}
			value = args[index]
		}
		if name == "product" {
			if seen {
				return "", errors.New("duplicate -product is ambiguous and is not allowed")
			}
			product = value
			seen = true
		}
	}
	return product, nil
}

func printSharedLearning(res learning.Response, err error) int {
	code := 0
	if err != nil {
		code = 2
		if errors.Is(err, learning.ErrInvalidGrant) || errors.Is(err, learning.ErrConflict) {
			code = 3
		}
		res.Error = err.Error()
		if res.Status == "" {
			res.Status = "error"
		}
	}
	if res.Version == "" {
		res.Version = learning.Version
	}
	if e := json.NewEncoder(os.Stdout).Encode(res); e != nil {
		return 2
	}
	return code
}

// Keep the established `air-worker learn` facade. A selected shared product never
// falls through to a legacy writer, even when an operation is not implemented yet.
func routeSharedLearn(argv []string) (bool, int) {
	if len(argv) == 0 {
		return false, 0
	}
	// Legacy apply accepts its proposal ID before flags. Normalize that one
	// spelling for the shared facade without changing the legacy invocation.
	if len(argv) > 1 && argv[0] == "apply" && !strings.HasPrefix(argv[1], "-") {
		normalized := []string{argv[0], "-id", argv[1]}
		argv = append(normalized, argv[2:]...)
	}
	root, routeErr := learningProductArg(argv)
	if routeErr != nil {
		return true, printSharedLearning(learning.Response{}, routeErr)
	}
	if root == "" {
		// Shared commands require an explicit product. The legacy parser must
		// not misreport a recognized action as an unknown action.
		switch argv[0] {
		case "status", "paths", "context", "load", "index", "events",
			"finalize", "summary", "module", "event", "add", "propose",
			"pending", "diff", "apply", "approve", "reject", "rollback",
			"effect", "review":
			for _, arg := range argv[1:] {
				if arg == "-h" || arg == "-help" || arg == "--help" {
					fmt.Fprintf(os.Stdout, "Usage: air-worker learn %s -product <root> [action options]\nThe -product flag is required for shared learning.\n", argv[0])
					return true, 0
				}
			}
			return true, printSharedLearning(learning.Response{}, errors.New("learn "+argv[0]+": -product is required; see air-worker learn --help"))
		}
		return false, 0
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return true, printSharedLearning(learning.Response{}, err)
	}
	s, on, err := readSharedLearningSettings(root)
	if !on && err == nil {
		return false, 0
	}
	if err != nil {
		return true, printSharedLearning(learning.Response{}, err)
	}
	return true, cmdSharedLearn(root, s, argv)
}

func cmdSharedLearn(root string, s sharedLearningSettings, argv []string) int {
	operation := argv[0]
	fs := flag.NewFlagSet("learn "+operation, flag.ContinueOnError)
	_ = fs.String("product", root, "корень продукта")
	_ = fs.Bool("json", true, "машиночитаемый JSON")
	dataFile := fs.String("data-file", "", "JSON запроса без конфигурации/секретов")
	runID := fs.String("run-id", "", "устойчивый ID прогона")
	kind := fs.String("kind", "", "тип записи/предложения")
	class := fs.String("class", "", "класс опыта")
	observed := fs.String("observed", "", "наблюдение/поправка")
	evidence := fs.String("evidence", "", "ссылка на квитанцию")
	source := fs.String("source", "worker", "источник наблюдения")
	actor := fs.String("actor", "", "идентификатор исполнителя")
	actorKind := fs.String("actor-kind", "gpt-window", "тип исполнителя")
	session := fs.String("session", "", "идентификатор сессии")
	reference := fs.String("reference", "", "ссылка на исходный материал")
	outcome := fs.String("outcome", "", "исход работы")
	ruleID := fs.String("rule-id", "", "точный ID/target@SHA реально использованной процедуры")
	id := fs.String("id", "", "proposal_id")
	target := fs.String("target", "", "относительный путь навыка")
	preSHA := fs.String("pre-sha256", "", "SHA до правки; пусто только для нового файла")
	contentFile := fs.String("content-file", "", "файл нового текста процедуры")
	_ = fs.String("transcript", "", "необязательный старый параметр; GPT его не требует")
	_ = fs.String("review-id", "", "старый ID review; модуль назначает собственную квитанцию")
	if err := fs.Parse(argv[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	data := map[string]any{}
	if *dataFile != "" {
		b, err := readLearningBounded(*dataFile, sharedLearningMaxBytes)
		if err != nil {
			return printSharedLearning(learning.Response{}, err)
		}
		if err = json.Unmarshal(b, &data); err != nil {
			return printSharedLearning(learning.Response{}, err)
		}
		if data == nil {
			return printSharedLearning(learning.Response{}, errors.New("request data must be a JSON object, not null"))
		}
	}
	if operation == "module" {
		b, err := io.ReadAll(io.LimitReader(os.Stdin, sharedLearningMaxBytes+1))
		if err != nil || len(b) > sharedLearningMaxBytes {
			return printSharedLearning(learning.Response{}, errors.New("module stdin exceeds limit"))
		}
		var req struct {
			Operation string         `json:"operation"`
			Data      map[string]any `json:"data"`
			Version   string         `json:"version"`
		}
		if err = json.Unmarshal(b, &req); err != nil {
			return printSharedLearning(learning.Response{}, err)
		}
		if req.Version != "" && req.Version != learning.Version {
			return printSharedLearning(learning.Response{}, errors.New("module request version mismatch"))
		}
		if req.Operation == "judge" {
			return printSharedLearning(learning.Response{}, errors.New("a caller-supplied verdict is not trusted; observe invokes the configured judge"))
		}
		return printSharedLearning(executeSharedLearning(root, s, req.Operation, req.Data))
	}
	if fs.NArg() != 0 {
		return printSharedLearning(learning.Response{}, errors.New("unexpected positional arguments in shared learning command"))
	}
	// Only explicitly supplied flags override a JSON field, including an explicit
	// empty string. Flag defaults must never erase a structured request.
	explicit := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	for flagName, item := range map[string]struct{ key, value string }{
		"run-id": {"run_id", *runID}, "id": {"proposal_id", *id}, "target": {"target", *target},
		"outcome": {"outcome", *outcome}, "rule-id": {"rule_id", *ruleID}, "observed": {"observed", *observed}, "class": {"class", *class},
		"source": {"source", *source}, "actor": {"actor", *actor}, "actor-kind": {"actor_kind", *actorKind},
		"session": {"session", *session}, "evidence": {"evidence", *evidence}, "reference": {"reference", *reference},
	} {
		if explicit[flagName] {
			data[item.key] = item.value
		}
	}
	switch operation {
	case "event", "add", "finalize":
		for _, key := range []string{"run_id", "observed", "class", "source", "actor", "actor_kind", "principal", "session", "evidence", "reference", "outcome_ref", "outcome", "version", "input_ref", "decision", "rule_id", "feedback"} {
			if value, exists := data[key]; exists {
				if _, ok := value.(string); !ok {
					return printSharedLearning(learning.Response{}, fmt.Errorf("event field %s must be a string", key))
				}
			}
		}
		text := func(key string) string { value, _ := data[key].(string); return value }
		rid := strings.TrimSpace(text("run_id"))
		if rid == "" && operation != "finalize" {
			var err error
			rid, err = newLearnID("LE", time.Now().UTC())
			if err != nil {
				return printSharedLearning(learning.Response{}, err)
			}
		}
		if rid == "" {
			return printSharedLearning(learning.Response{}, errors.New("stable run_id is required for finalize"))
		}
		ref := text("outcome_ref")
		switch {
		case explicit["evidence"]:
			ref = *evidence
		case explicit["reference"]:
			ref = *reference
		case ref == "" && text("evidence") != "":
			ref = text("evidence")
		case ref == "":
			ref = text("reference")
		}
		who := text("principal")
		if explicit["actor"] || explicit["actor-kind"] || text("actor") != "" || who == "" {
			k := text("actor_kind")
			if _, exists := data["actor_kind"]; !exists {
				k = *actorKind
			}
			var err error
			who, err = resolveMutationActor(k, text("actor"), "")
			if err != nil {
				return printSharedLearning(learning.Response{}, err)
			}
		}
		if _, exists := data["source"]; !exists {
			data["source"] = *source
		}
		data["run_id"], data["principal"], data["outcome_ref"] = rid, who, ref
		data["kind"] = "run_completed"
		return printSharedLearning(executeSharedLearning(root, s, "observe", data))
	case "propose":
		if *contentFile != "" {
			b, err := readLearningBounded(*contentFile, 64*1024)
			if err != nil {
				return printSharedLearning(learning.Response{}, err)
			}
			data["content"] = string(b)
		}
		if *kind != "" {
			data["kind"] = *kind
		} else if data["kind"] == nil {
			data["kind"] = "procedure"
		}
		specifiedPre := false
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "pre-sha256" {
				specifiedPre = true
			}
		})
		if specifiedPre || data["pre_sha256"] == nil {
			data["pre_sha256"] = *preSHA
		}
	case "context":
		if targetValue, _ := data["target"].(string); targetValue == "" {
			return printSharedLearning(executeSharedLearning(root, s, "index", data))
		}
		operation = "load"
	case "paths":
		operation = "status"
	case "status", "pending", "diff", "apply", "approve", "reject", "rollback", "review", "effect", "summary", "load", "index", "events":
	default:
		return printSharedLearning(learning.Response{}, fmt.Errorf("shared learning: operation %q is not supported; legacy state will not be mutated", operation))
	}
	return printSharedLearning(executeSharedLearning(root, s, operation, data))
}

func routeSharedFeedback(argv []string) (bool, int) {
	root, routeErr := learningProductArg(argv)
	if routeErr != nil {
		return true, printSharedLearning(learning.Response{}, routeErr)
	}
	if root == "" {
		return false, 0
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return true, printSharedLearning(learning.Response{}, err)
	}
	s, on, err := readSharedLearningSettings(root)
	if !on && err == nil {
		return false, 0
	}
	if err != nil {
		return true, printSharedLearning(learning.Response{}, err)
	}
	action := "add"
	args := argv
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		action = args[0]
		args = args[1:]
	}
	fs := flag.NewFlagSet("feedback "+action, flag.ContinueOnError)
	_ = fs.String("product", root, "корень продукта")
	_ = fs.Bool("json", true, "машиночитаемый журнал")
	kind := fs.String("kind", "", "error|idea|lesson")
	text := fs.String("text", "", "текст обратной связи")
	source := fs.String("source", "worker", "источник")
	ref := fs.String("ref", "", "ссылка на квитанцию")
	rid := fs.String("run-id", "", "ID этой записи для повторной доставки")
	dataFile := fs.String("data-file", "", "абсолютный путь к ограниченному JSON вводу без потери кавычек")
	useProcedure := fs.String("use-procedure", "", "явно выбранный ранее выученный безопасный target@SHA256 для машинно проверяемой штатной операции feedback")
	oldObserved := fs.String("observed", "", "совместимый старый параметр")
	oldEvidence := fs.String("evidence", "", "совместимый старый параметр")
	legacyFields := map[string]*string{}
	for _, name := range []string{"type", "severity", "source-version", "expected", "reproduction", "workaround", "proposed-outcome"} {
		legacyFields[name] = fs.String(name, "", "совместимый старый параметр; сохраняется в feedback metadata")
	}
	if err := fs.Parse(args); err != nil {
		return true, 2
	}
	if fs.NArg() != 0 {
		return true, printSharedLearning(learning.Response{}, errors.New("feedback: unexpected positional argument; nested Windows quoting may have truncated -text; use -data-file JSON"))
	}
	dataInputSHA := ""
	if *dataFile != "" {
		var invalidFlag string
		fs.Visit(func(f *flag.Flag) {
			if f.Name != "product" && f.Name != "json" && f.Name != "data-file" {
				invalidFlag = f.Name
			}
		})
		if invalidFlag != "" {
			return true, printSharedLearning(learning.Response{}, fmt.Errorf("feedback -data-file conflicts with inline -%s", invalidFlag))
		}
		values, sha, readErr := readNativeFeedbackRequest(*dataFile)
		if readErr != nil {
			return true, printSharedLearning(learning.Response{}, readErr)
		}
		dataInputSHA = sha
		*text, *kind, *source, *ref, *rid, *useProcedure = values["text"], values["kind"], values["source"], values["ref"], values["run_id"], values["use_procedure"]
		if *source == "" {
			*source = "worker"
		}
		for name, ptr := range legacyFields {
			if value, exists := values[name]; exists {
				*ptr = value
			}
		}
		legacyFields["input-sha256"] = &dataInputSHA
	}
	if action == "list" {
		if *dataFile != "" {
			return true, printSharedLearning(learning.Response{}, errors.New("feedback list does not accept -data-file"))
		}
		return true, printSharedLearning(executeSharedLearning(root, s, "events", map[string]string{"source": "feedback"}))
	}
	if action != "add" {
		return true, printSharedLearning(learning.Response{}, errors.New("feedback expects add or list"))
	}
	if *text == "" {
		*text = *oldObserved
	}
	if *ref == "" {
		*ref = *oldEvidence
	}
	if strings.TrimSpace(*text) == "" {
		return true, printSharedLearning(learning.Response{}, errors.New("feedback text is required"))
	}
	if len([]byte(*text)) > 64*1024 {
		return true, printSharedLearning(learning.Response{}, errors.New("feedback text exceeds the bounded immutable receipt limit"))
	}
	if *rid == "" {
		*rid, err = newLearnID("FB", time.Now().UTC())
		if err != nil {
			return true, printSharedLearning(learning.Response{}, err)
		}
	}
	if *kind == "" {
		switch *legacyFields["type"] {
		case "defect", "friction":
			*kind = "error"
		case "idea":
			*kind = "idea"
		default:
			*kind = "lesson"
		}
	}
	if *kind != "error" && *kind != "idea" && *kind != "lesson" {
		return true, printSharedLearning(learning.Response{}, errors.New("feedback kind must be error, idea or lesson"))
	}
	metadata := map[string]string{"kind": *kind, "source": *source, "text": *text, "ref": *ref}
	for name, value := range legacyFields {
		if *value != "" {
			metadata[name] = *value
		}
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return true, printSharedLearning(learning.Response{}, err)
	}
	if *useProcedure != "" {
		// Stable source/run ownership spans selection, native operation and
		// usage evidence. The lock is OS-backed and cross-process on Windows.
		useLock, acquired := acquireLock(lockName("native-feedback-use", *source+"\n"+*rid))
		if !acquired {
			return true, printSharedLearning(learning.Response{}, errors.New("another learned feedback use owns this source/run_id"))
		}
		defer useLock.release()
	}
	claim, err := prepareNativeFeedbackUse(*useProcedure, *source, *rid, s)
	if err != nil {
		return true, printSharedLearning(learning.Response{}, err)
	}
	res, err := executeSharedLearning(root, s, "observe", map[string]string{"run_id": *rid, "kind": "run_completed", "observed": *text, "class": "feedback-" + *kind, "source": "feedback", "principal": *source, "outcome_ref": *ref, "feedback": string(encoded)})
	if err != nil && !(errors.Is(err, learning.ErrConflict) && res.Status == "duplicate") {
		// A reviewer/model error may occur AFTER the shared module durably
		// records this feedback. Preserve that real event as a canonical
		// candidate even when review is deferred or failed. Never treat
		// the model error as an applied lesson or a passing review.
		_, intakeErr := preserveCanonicalSharedFeedback(root, s, *rid, string(encoded), *kind, *text, *source, *ref, legacyFields)
		res.Status = "partial"
		if intakeErr != nil {
			return true, printSharedLearning(res, errors.Join(err, intakeErr))
		}
		return true, printSharedLearning(res, fmt.Errorf("canonical feedback candidate recorded; learning review remains incomplete: %w", err))
	}
	// Complete the product's canonical immutable defect intake only after
	// the shared module durably recorded the exact event. On a partial
	// failure, the same run-id can resume without another event or overwrite.
	_, err = preserveCanonicalSharedFeedback(root, s, *rid, string(encoded), *kind, *text, *source, *ref, legacyFields)
	if err != nil {
		res.Status = "partial"
		return true, printSharedLearning(res, fmt.Errorf("learning event persisted but canonical feedback candidate needs recovery (run_id=%s): %w", *rid, err))
	}
	if claim != nil {
		event, evidenceErr := findSharedFeedbackEvent(s.RuntimeRoot, *rid, string(encoded), *text, *source)
		if evidenceErr == nil {
			evidenceErr = recordNativeFeedbackProcedureUse(claim, root, event["event_id"])
		}
		if evidenceErr != nil {
			res.Status = "partial"
			return true, printSharedLearning(res, fmt.Errorf("native learned feedback use not proven (run_id=%s): %w", *rid, evidenceErr))
		}
	}
	if res.Status == "duplicate" {
		res.Status = "recorded"
	}
	return true, printSharedLearning(res, nil)
}
