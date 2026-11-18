package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSharedPromptRelevancePrefersMatchingOldSkill(t *testing.T) {
	recent := sharedLearningCatalogSkill{
		Target: "skills/learned/native-judge-setup.md", Title: "Configure the native judge",
		SHA256: strings.Repeat("b", 64),
	}
	older := sharedLearningCatalogSkill{
		Target: "skills/learned/prior-lesson.md",
		Title:  "Reconcile execution unknown before retry",
		SHA256: strings.Repeat("a", 64),
	}
	input := []sharedLearningCatalogSkill{recent, older}
	got := selectSharedSkillsForTurn(input, "A command returned EXECUTION_UNKNOWN. Check before retry.")
	if !reflect.DeepEqual(got, []sharedLearningCatalogSkill{older}) {
		t.Fatalf("turn must carry only the matching procedure: %+v", got)
	}
	if !reflect.DeepEqual(input, []sharedLearningCatalogSkill{recent, older}) {
		t.Fatal("relevance selection modified the original newest-first list")
	}
}

func TestSharedPromptRelevanceRanksByScoreThenRecency(t *testing.T) {
	newest := sharedLearningCatalogSkill{Target: "skills/learned/a.md", Title: "Retry policy"}
	middle := sharedLearningCatalogSkill{Target: "skills/learned/b.md", Title: "Encoding retry policy"}
	oldest := sharedLearningCatalogSkill{Target: "skills/learned/c.md", Title: "Retry policy notes"}
	got := selectSharedSkillsForTurn([]sharedLearningCatalogSkill{newest, middle, oldest}, "fix encoding retry policy")
	if !reflect.DeepEqual(got, []sharedLearningCatalogSkill{middle, newest, oldest}) {
		t.Fatalf("higher score first, ties newest first: %+v", got)
	}
}

func TestSharedPromptRelevanceFallsBackToTargetSlug(t *testing.T) {
	latest := sharedLearningCatalogSkill{Target: "skills/learned/other.md", Title: "General task"}
	older := sharedLearningCatalogSkill{Target: "skills/learned/encoding-gb2312.md", Title: ""}
	got := selectSharedSkillsForTurn([]sharedLearningCatalogSkill{latest, older},
		"Inspect encoding GB2312 before the next import")
	if !reflect.DeepEqual(got, []sharedLearningCatalogSkill{older}) {
		t.Fatalf("target metadata not used when title absent: %+v", got)
	}
}

func TestSharedPromptRelevanceDeliversNothingForEmptyGenericOrUnrelatedInput(t *testing.T) {
	latest := sharedLearningCatalogSkill{Target: "skills/learned/first.md", Title: "First skill"}
	older := sharedLearningCatalogSkill{Target: "skills/learned/second.md", Title: "Second skill"}
	input := []sharedLearningCatalogSkill{latest, older}
	for _, prompt := range []string{"", "air worker native skill failure", "a task with no matching detail"} {
		if got := selectSharedSkillsForTurn(input, prompt); len(got) != 0 {
			t.Fatalf("unrelated turn got procedures for %q: %+v", prompt, got)
		}
	}
}

func TestSharedPromptRelevanceIgnoresFunctionWords(t *testing.T) {
	config := sharedLearningCatalogSkill{Target: "skills/learned/run-config.md", Title: "Create Run Configuration Before Retrying"}
	unknown := sharedLearningCatalogSkill{Target: "skills/learned/unknown.md", Title: "Reconcile execution unknown"}
	got := selectSharedSkillsForTurn([]sharedLearningCatalogSkill{config, unknown}, "EXECUTION_UNKNOWN: reconcile before retry")
	if !reflect.DeepEqual(got, []sharedLearningCatalogSkill{unknown}) {
		t.Fatalf("a function word pulled an unrelated procedure: %+v", got)
	}
	logs := sharedLearningCatalogSkill{Target: "skills/learned/incident.md", Title: "Collect logs during incident recovery"}
	if got := selectSharedSkillsForTurn([]sharedLearningCatalogSkill{logs}, "What happened during the release?"); len(got) != 0 {
		t.Fatalf("'during' alone matched: %+v", got)
	}
	start := sharedLearningCatalogSkill{Target: "skills/learned/start.md", Title: "Couldn't start service"}
	if got := selectSharedSkillsForTurn([]sharedLearningCatalogSkill{start}, "We couldn't reproduce it"); len(got) != 0 {
		t.Fatalf("contraction stem matched: %+v", got)
	}
	ru := sharedLearningCatalogSkill{Target: "skills/learned/ru.md", Title: "Проверить отчёт перед отправкой"}
	why := sharedLearningCatalogSkill{Target: "skills/learned/why.md", Title: "Зачем это нужно"}
	who := sharedLearningCatalogSkill{Target: "skills/learned/who.md", Title: "Чего ждать от тебя тебе"}
	for _, w := range strings.Fields(learningStopWords) {
		if got := selectSharedSkillsForTurn([]sharedLearningCatalogSkill{{Target: "skills/learned/x.md", Title: w}}, w); len(got) != 0 {
			t.Fatalf("stop word %q matched", w)
		}
	}
	for _, prompt := range []string{"сделай это перед обедом", "Зачем?", "чего тебе тебя"} {
		if got := selectSharedSkillsForTurn([]sharedLearningCatalogSkill{who}, prompt); len(got) != 0 {
			t.Fatalf("Russian function word matched for %q: %+v", prompt, got)
		}
		if got := selectSharedSkillsForTurn([]sharedLearningCatalogSkill{ru, why}, prompt); len(got) != 0 {
			t.Fatalf("Russian function word matched for %q: %+v", prompt, got)
		}
	}
}

func TestSharedPromptRelevanceSupportsRussianTitleAndBoundedPrompt(t *testing.T) {
	first := sharedLearningCatalogSkill{Target: "skills/learned/other.md", Title: "Проверка стоимости"}
	older := sharedLearningCatalogSkill{Target: "skills/learned/encoding.md", Title: "Исправление кодировки отчёта"}
	input := []sharedLearningCatalogSkill{first, older}
	if got := selectSharedSkillsForTurn(input, "Перепроверь кодировки в отчёте"); !reflect.DeepEqual(got, []sharedLearningCatalogSkill{older}) {
		t.Fatalf("Cyrillic title not selected: %+v", got)
	}
	// A very long untrusted prompt cannot force scanning arbitrary trailing
	// text after the deliberately bounded prefix.
	long := strings.Repeat("x", maxLearningRelevancePromptBytes) + " кодировки"
	if got := selectSharedSkillsForTurn(input, long); len(got) != 0 {
		t.Fatalf("unbounded prompt tail changed selection: %+v", got)
	}
}

func seedPromptRelevanceSkills(t *testing.T, product string, settings sharedLearningSettings, target string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("skills/learned/recent-workflow-%02d.md", i)
		body := fmt.Sprintf("# Generic workflow %d\n\n## When to apply\nWhen handling generic step %d.\n\n## Procedure\n1. Check step %d.\n\n## Pitfalls\nDo not invent results.\n", i, i, i)
		if i == 0 {
			name = target
			body = "# Resolve execution unknown before retry\n\n## When to apply\nWhen an execution result is unknown.\n\n## Procedure\n1. Inspect the original native receipt before retry.\n\n## Pitfalls\nDo not turn unknown into failure.\n"
		}
		res, err := executeSharedLearning(product, settings, "propose", map[string]string{
			"proposal_id": fmt.Sprintf("LP-prompt-relevance-%02d", i),
			"kind":        "procedure", "target": name, "pre_sha256": "", "content": body,
		})
		if err != nil || res.Status != "applied" {
			t.Fatalf("seed %d: %+v %v", i, res, err)
		}
	}
}

func TestSharedUserPromptDeliversRelevantOldSkillFromExistingModuleIndex(t *testing.T) {
	product, settings := sharedProductFixture(t, false)
	target := "skills/learned/older-guidance.md"
	seedPromptRelevanceSkills(t, product, settings, target, 11)
	in := hookInput{
		Principal: "claude", SessionID: "prompt-relevance", RunID: "relevant-session",
		HookEventName: "UserPromptSubmit", Prompt: "EXECUTION_UNKNOWN: reconcile before retry",
	}
	handled, res, err := sharedLearningContext(product, in)
	if !handled || err != nil {
		t.Fatalf("UserPromptSubmit context unavailable: handled=%v err=%v", handled, err)
	}
	if len(res.LoadedSkills) != 1 || res.LoadedSkills[0].Target != target {
		t.Fatalf("turn must carry only the matching older skill: %+v", res.LoadedSkills)
	}
	if !strings.Contains(res.Context, "Resolve execution unknown before retry") || strings.Contains(res.Context, "Generic workflow") {
		t.Fatalf("turn context is not limited to the task class:\n%s", res.Context)
	}
	if got := sharedUsageCount(t, settings.RuntimeRoot, "skill_loaded"); got != 1 {
		t.Fatalf("context did not record one native loaded receipt: %d", got)
	}
	in.Prompt = "rename the release notes heading"
	handled, res, err = sharedLearningContext(product, in)
	if !handled || err != nil || res.Context != "" || len(res.LoadedSkills) != 0 {
		t.Fatalf("unrelated turn got procedures: handled=%v err=%v %+v", handled, err, res)
	}
	if got := sharedUsageCount(t, settings.RuntimeRoot, "skill_loaded"); got != 1 {
		t.Fatalf("unrelated turn recorded a load: %d", got)
	}
}

// An unmatched turn keeps the receipt, so a receipt of another owner can
// survive: PostToolUse must not treat its loads as this owner's.
func TestA4ForeignOwnerReceiptProvesNoLoad(t *testing.T) {
	_, _, stateDir := selfLearningFixture(t)
	target := filepath.Join(t.TempDir(), "other-product")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	in := declareLearningTestSession(t, stateDir, "claude", "a4-foreign-owner", target)
	in.RunID = "a4-foreign-owner-run"
	if _, err := handleLearningContext(in); err != nil {
		t.Fatal(err)
	}
	rec, path, found, err := readSelfLearningContextReceipt(in)
	if err != nil || !found || len(rec.Loaded) == 0 {
		t.Fatalf("loaded context unavailable: %+v found=%v err=%v", rec, found, err)
	}
	for _, foreign := range []func(*selfLearningContextReceipt){
		func(r *selfLearningContextReceipt) { r.ProductRoot = t.TempDir() },
		func(r *selfLearningContextReceipt) { r.RuntimeRoot = t.TempDir() },
		func(r *selfLearningContextReceipt) { r.ConfigSHA = strings.Repeat("0", 64) },
		func(r *selfLearningContextReceipt) { r.ConfigSHA = "" },
	} {
		other := rec
		foreign(&other)
		if err := writeSelfLearningContextReceipt(path, other); err != nil {
			t.Fatal(err)
		}
		unknown := n093TestHook(in, "mcp__AIR_Commander_Test__start_process",
			"{\"node\":\"SRVLM01\",\"command\":\"release mutation\"}",
			"{\"error\":\"EXECUTION_UNKNOWN\",\"pid\":4242}")
		if res, err := handlePostToolUseSelfLearning(unknown); err != nil || res.Context != "" {
			t.Fatalf("foreign receipt drove PostToolUse: %+v %v", res, err)
		}
		if _, _, exists, err := readSelfLearningTriggerReceipt(in); err != nil || exists {
			t.Fatalf("foreign receipt triggered: exists=%v err=%v", exists, err)
		}
	}
}

// The owner changing while a turn waits for the receipt lock records nothing.
func TestA4OwnerChangeBeforeReceiptWriteRecordsNothing(t *testing.T) {
	selfLearningFixture(t)
	in := hookInput{Principal: "claude", SessionID: "a4-owner-change", HookEventName: "UserPromptSubmit",
		Prompt: "EXECUTION_UNKNOWN: reconcile before retry"}
	saved := autoSelfDeliveryAcquire
	t.Cleanup(func() { autoSelfDeliveryAcquire = saved })
	autoSelfDeliveryAcquire = func(name string) (*osLock, bool) {
		sel, found, err := readAirWorkerSelfLearningSelector()
		if err != nil || !found {
			t.Fatalf("selector: %v", err)
		}
		sel.ConfigSHA256 = strings.Repeat("0", 64)
		if err := writeAirWorkerSelfLearningSelector(sel); err != nil {
			t.Fatal(err)
		}
		return saved(name)
	}
	if _, _, _, err := airWorkerSelfLearningContext(in); err == nil {
		t.Fatal("delivery for a stale owner was not refused")
	}
	if _, _, found, err := readSelfLearningContextReceipt(in); err != nil || found {
		t.Fatalf("receipt written for a stale owner: found=%v err=%v", found, err)
	}
}

// A4 load accounting: a later turn adds to the session snapshot that
// SessionStart wrote and an unrelated turn leaves it untouched.
func TestA4TurnDeliveryKeepsSessionLoadAccounting(t *testing.T) {
	product, settings, _ := selfLearningFixture(t)
	target := "skills/learned/older-guidance.md"
	// Nine procedures: SessionStart delivers the 8 newest, not the oldest.
	seedPromptRelevanceSkills(t, product, settings, target, 9)
	in := hookInput{Principal: "claude", SessionID: "a4-turn-accounting", HookEventName: "SessionStart"}
	if _, ctx, on, err := airWorkerSelfLearningContext(in); err != nil || !on || strings.Contains(ctx, "Resolve execution unknown") {
		t.Fatalf("SessionStart: on=%v err=%v oldest delivered=%v", on, err, strings.Contains(ctx, "Resolve execution unknown"))
	}
	start, path, found, err := readSelfLearningContextReceipt(in)
	if err != nil || !found || len(start.Loaded) != 8 {
		t.Fatalf("SessionStart snapshot: found=%v err=%v %+v", found, err, start.Loaded)
	}
	before, _ := os.ReadFile(path)

	in.HookEventName, in.Prompt = "UserPromptSubmit", "rename the release notes heading"
	if _, ctx, _, err := airWorkerSelfLearningContext(in); err != nil || ctx != "" {
		t.Fatalf("unrelated turn: err=%v ctx=%q", err, ctx)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("unrelated turn rewrote the session snapshot")
	}

	in.Prompt = "EXECUTION_UNKNOWN: reconcile before retry"
	_, ctx, _, err := airWorkerSelfLearningContext(in)
	if err != nil || !strings.Contains(ctx, "Resolve execution unknown") || strings.Contains(ctx, "Generic workflow") {
		t.Fatalf("matching turn: err=%v ctx=%q", err, ctx)
	}
	turn, _, _, err := readSelfLearningContextReceipt(in)
	if err != nil {
		t.Fatal(err)
	}
	got := map[selfLearningLoadedRule]bool{}
	hasTarget := false
	for _, r := range turn.Loaded {
		got[r] = true
		hasTarget = hasTarget || r.Target == target
	}
	for _, r := range start.Loaded {
		if !got[r] {
			t.Fatalf("turn dropped SessionStart load %+v: %+v", r, turn.Loaded)
		}
	}
	if !hasTarget || len(turn.Loaded) <= len(start.Loaded) {
		t.Fatalf("turn delivery not added: %+v", turn.Loaded)
	}

	// A snapshot of another product or runtime under the same session/run is
	// never merged into this owner's snapshot.
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on {
		t.Fatalf("owner: %v %v", on, err)
	}
	foreignRule := selfLearningLoadedRule{Target: "skills/learned/foreign.md", SHA256: strings.Repeat("f", 64)}
	for _, foreign := range []selfLearningContextReceipt{
		{ProductRoot: t.TempDir(), RuntimeRoot: owner.Settings.RuntimeRoot},
		{ProductRoot: owner.Selector.ProductRoot, RuntimeRoot: t.TempDir()},
	} {
		foreign.Schema, foreign.Principal, foreign.Session, foreign.RunID = selfLearningContextReceiptSchema, hookPrincipal(in), in.SessionID, selfLearningRunID(in)
		foreign.Loaded = []selfLearningLoadedRule{foreignRule}
		if err := writeSelfLearningContextReceipt(path, foreign); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := airWorkerSelfLearningContext(in); err != nil {
			t.Fatal(err)
		}
		mixed, _, _, err := readSelfLearningContextReceipt(in)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range mixed.Loaded {
			if r == foreignRule {
				t.Fatalf("foreign owner snapshot merged: %+v", mixed.Loaded)
			}
		}
	}
}

func TestSharedPromptRelevanceTechnicalTermsAndGenericActions(t *testing.T) {
	cli := sharedLearningCatalogSkill{Target: "skills/learned/cli.md", Title: "CLI"}
	api := sharedLearningCatalogSkill{Target: "skills/learned/api.md", Title: "API"}
	sdk := sharedLearningCatalogSkill{Target: "skills/learned/sdk.md", Title: "SDK"}
	mcp := sharedLearningCatalogSkill{Target: "skills/learned/mcp.md", Title: "MCP"}
	deployment := sharedLearningCatalogSkill{Target: "skills/learned/deployment.md", Title: "Retry failed deployment"}
	payment := sharedLearningCatalogSkill{Target: "skills/learned/payment.md", Title: "Retry failed payment"}
	database := sharedLearningCatalogSkill{Target: "skills/learned/database.md", Title: "Repair database"}
	network := sharedLearningCatalogSkill{Target: "skills/learned/network.md", Title: "Repair network"}
	for _, tc := range []struct {
		prompt       string
		skills, want []sharedLearningCatalogSkill
	}{
		{"CLI", []sharedLearningCatalogSkill{cli, api}, []sharedLearningCatalogSkill{cli}},
		{"API", []sharedLearningCatalogSkill{cli, api}, []sharedLearningCatalogSkill{api}},
		{"SDK", []sharedLearningCatalogSkill{sdk, mcp}, []sharedLearningCatalogSkill{sdk}},
		{"MCP", []sharedLearningCatalogSkill{sdk, mcp}, []sharedLearningCatalogSkill{mcp}},
		{"retry failed payment", []sharedLearningCatalogSkill{deployment, payment}, []sharedLearningCatalogSkill{payment}},
		{"repair network", []sharedLearningCatalogSkill{database, network}, []sharedLearningCatalogSkill{network}},
		{"retry failed", []sharedLearningCatalogSkill{deployment, payment}, nil},
	} {
		if got := selectSharedSkillsForTurn(tc.skills, tc.prompt); !reflect.DeepEqual(got, tc.want) && !(len(got) == 0 && len(tc.want) == 0) {
			t.Errorf("%q: got %+v, want %+v", tc.prompt, got, tc.want)
		}
	}
}
