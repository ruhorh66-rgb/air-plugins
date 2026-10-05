package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCriterion40Pending — К40: GATED отделён от UNKNOWN/NOT_PROVEN, executable distance и
// число LPR gates показываются раздельно, закрытые gates не считаются work.
func TestCriterion40Pending(t *testing.T) {
	root := t.TempDir()
	plan := filepath.Join(root, "PLAN.md")
	text := "" +
		"**Ц1.** цель проверяется двумя критериями с разными мерами\n\n" +
		"| Критерий | Цель | Признак достижения | Чем меряется |\n" +
		"|---|---|---|---|\n" +
		"| К1 | Ц1 | решение ЛПР ждёт | факт `f1` |\n" +
		"| К2 | Ц1 | измерить нечем | факт `f2` |\n\n" +
		"| № | Шаг | Ступень | Судья |\n" +
		"|---|---|---|---|\n" +
		"| 1 | работа | `sonnet` | исполнитель: ведущая |\n" +
		"| 2 | открытый гейт | — | гейт: ЛПР |\n" +
		"| ~~3~~ | закрытый гейт | — | гейт: ЛПР |\n"
	if err := os.WriteFile(plan, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	checklist := filepath.Join(root, "checklist.json")
	cl := checklistFile{Items: []factItem{
		{ID: "f1", Status: "gated", Awaits: "решение ЛПР по f1"},
		// f2 отсутствует в реестре вовсе — критерию К2 нечем измериться.
	}}
	b, _ := json.Marshal(cl)
	if err := os.WriteFile(checklist, b, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := runConfig{Plan: "PLAN.md", Judge: judgeSpec{Checklist: "checklist.json"}}

	ps := buildPlanState(root, cfg, plan, judgeResult{})

	if len(ps.CriteriaGated) != 1 || !strings.HasPrefix(ps.CriteriaGated[0], "К1") {
		t.Fatalf("К1 (факт gated) обязан попасть в CriteriaGated, а не в unknown: %+v", ps.CriteriaGated)
	}
	for _, u := range ps.CriteriaUnknown {
		if strings.HasPrefix(u, "К1") {
			t.Fatalf("GATED критерий не должен дублироваться в CriteriaUnknown: %+v", ps.CriteriaUnknown)
		}
	}
	if len(ps.CriteriaUnknown) != 1 || !strings.HasPrefix(ps.CriteriaUnknown[0], "К2") {
		t.Fatalf("К2 (нечем измерить) обязан попасть в CriteriaUnknown: %+v", ps.CriteriaUnknown)
	}

	// Закрытый гейт плана (шаг 3) не открытое ожидание — он сделан, и не входит ни в
	// PlanGates, ни в LPRGates, ни в ExecutableWork.
	if ps.PlanGates != 1 {
		t.Fatalf("PlanGates обязан считать только ОТКРЫТЫЙ гейт плана (шаг 2), получено %d", ps.PlanGates)
	}
	if ps.ExecutableWork != 1 {
		t.Fatalf("ExecutableWork обязан считать только шаг 1 (не гейт), получено %d", ps.ExecutableWork)
	}
	if ps.LPRGates != ps.PlanGates+len(ps.CriteriaGated) {
		t.Fatalf("LPRGates обязан быть суммой открытых гейтов плана и GATED-критериев: PlanGates=%d CriteriaGated=%d LPRGates=%d",
			ps.PlanGates, len(ps.CriteriaGated), ps.LPRGates)
	}
	if ps.LPRGates != 2 {
		t.Fatalf("LPRGates ожидался 2 (1 открытый гейт плана + 1 GATED критерий), получено %d", ps.LPRGates)
	}
	if ps.TechnicalUnknowns != len(ps.CriteriaUnknown) {
		t.Fatalf("TechnicalUnknowns обязан считать ровно CriteriaUnknown, получено %d при %d unknown",
			ps.TechnicalUnknowns, len(ps.CriteriaUnknown))
	}
}

// TestCriterion59Pending — К59: distance не считает одну обязанность дважды. Факт, уже
// являющийся мерой критерия, дедуплицирован против legacy min_facts; report отдельно
// показывает executable work, LPR gates и technical unknowns и предупреждает о legacy overlap.
func TestPlanStateUsesCanonicalPlanNodesOverLegacyRows(t *testing.T) {
	root := t.TempDir()
	plan := filepath.Join(root, "PLAN.md")
	text := "" +
		"**Ц1.** node source\n\n" +
		"| Критерий | Цель | Признак достижения | Чем меряется |\n" +
		"|---|---|---|---|\n" +
		"| К1 | Ц1 | fact | факт `f1` |\n\n" +
		"| № | Шаг | Ступень | Судья |\n" +
		"|---|---|---|---|\n" +
		"| 1 | legacy work one | `script` | К1 |\n" +
		"| 2 | legacy work two | `script` | К1 |\n" +
		"| 3 | historical release gate | — | гейт: ЛПР |\n"
	if err := os.WriteFile(plan, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(planNodeDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	nodes := []planNode{
		{ID: "N-001_open", Title: "Open", Parent: "root", Trigger: "test", Owner: "worker", DoneWhen: "done", Status: "open", ReturnTo: "root", CreatedAt: now, UpdatedAt: now},
		{ID: "N-002_closed", Title: "Closed", Parent: "root", Trigger: "test", Owner: "worker", DoneWhen: "done", Status: "closed", ReturnTo: "root", Receipts: []string{"receipt.json"}, CreatedAt: now, UpdatedAt: now},
	}
	for _, node := range nodes {
		if err := os.WriteFile(filepath.Join(planNodeDir(root), node.ID+".md"), renderPlanNode(node), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cl := checklistFile{Items: []factItem{{ID: "f1", Status: "completed"}}}
	raw, _ := json.Marshal(cl)
	if err := os.WriteFile(filepath.Join(root, "checklist.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := runConfig{Plan: "PLAN.md", Judge: judgeSpec{Checklist: "checklist.json"}}
	ps := buildPlanState(root, cfg, plan, judgeResult{})
	if ps.WorkSource != planWorkSourceNodes {
		t.Fatalf("work source=%q want %q", ps.WorkSource, planWorkSourceNodes)
	}
	if ps.ExecutableWork != 1 {
		t.Fatalf("node-spine open work=%d want 1; legacy table has two open rows and must not be active", ps.ExecutableWork)
	}
	if ps.PlanGates != 0 || ps.LPRGates != 0 {
		t.Fatalf("historical legacy gate leaked into node-source PlanState: PlanGates=%d LPRGates=%d", ps.PlanGates, ps.LPRGates)
	}
}

func TestCriterion59Pending(t *testing.T) {
	root := t.TempDir()
	plan := filepath.Join(root, "PLAN.md")
	text := "" +
		"**Ц1.** факт f1 меряет критерий и не должен считаться недостачей дважды\n\n" +
		"| Критерий | Цель | Признак достижения | Чем меряется |\n" +
		"|---|---|---|---|\n" +
		"| К1 | Ц1 | f1 закрыт | факт `f1` |\n\n" +
		"| № | Шаг | Ступень | Судья |\n" +
		"|---|---|---|---|\n" +
		"| 1 | работа | `sonnet` | К1: исполнитель |\n"
	if err := os.WriteFile(plan, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	checklist := filepath.Join(root, "checklist.json")
	cl := checklistFile{Items: []factItem{
		// f1 — мера критерия К1 И одновременно считается legacy min_facts; не закрыт.
		{ID: "f1", Status: "pending"},
		// f2 — только legacy min_facts, критерию не принадлежит; тоже не закрыт.
		{ID: "f2", Status: "pending"},
	}}
	b, _ := json.Marshal(cl)
	if err := os.WriteFile(checklist, b, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := runConfig{
		Plan:  "PLAN.md",
		Judge: judgeSpec{Checklist: "checklist.json", MinFacts: 2},
	}

	r := runJudge(root, cfg, -1, legacyScope(root))

	found := false
	for _, id := range r.FactsOverlap {
		if id == "f1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("f1 — мера критерия и одновременно предмет legacy min_facts, обязан попасть в FactsOverlap: %+v", r.FactsOverlap)
	}
	if len(r.CriteriaFailed) != 1 {
		t.Fatalf("К1 (факт f1 не закрыт) обязан провалиться ровно один раз: %+v", r.CriteriaFailed)
	}

	code, _ := verdict(r)
	d := distanceOf(code, r)
	if d == nil {
		t.Fatal("distance не обязан быть неизвестен при code!=2")
	}
	// Без дедупликации: 1 (CriteriaFailed) + 2 (обоих фактов не хватает по legacy min_facts) = 3.
	// С дедупликацией (К59): f1 уже посчитан критерием, значит недостача по нему не входит в
	// legacy-счёт второй раз: короткая недостача = FactsRequired(2) - FactsClosed(0) -
	// FactsGated(0) - len(FactsOverlap)(1) = 1, итог 1 (CriteriaFailed) + 1 (short f2) = 2.
	if *d != 2 {
		t.Fatalf("distance обязан считать f1 один раз (критерием), а не дважды (критерием и legacy min_facts): получено %d, ожидалось 2", *d)
	}
	if !strings.Contains(r.FactsLine, "не дублируются") {
		t.Fatalf("FactsLine обязана предупреждать о legacy overlap: %q", r.FactsLine)
	}
}
