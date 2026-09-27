package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const curatorSchemaVersion = "air-worker.curator/v1"

type curatorStepView struct {
	Num   string `json:"num"`
	Title string `json:"title"`
	Tier  string `json:"tier,omitempty"`
	Judge string `json:"judge,omitempty"`
	Gate  bool   `json:"gate,omitempty"`
}

type curatorEffectView struct {
	ProposalID   string `json:"proposal_id"`
	Class        string `json:"class"`
	AppliedAt    string `json:"applied_at"`
	RepeatsAfter int    `json:"repeats_after"`
	Verdict      string `json:"verdict"`
}

type curatorSnapshot struct {
	Schema      string              `json:"schema"`
	GeneratedAt string              `json:"generated_at"`
	Product     string              `json:"product"`
	Plan        string              `json:"plan"`
	OpenSteps   int                 `json:"open_steps"`
	NextStep    *curatorStepView    `json:"next_step,omitempty"`
	NextGate    *curatorStepView    `json:"next_gate,omitempty"`
	StepsToGate int                 `json:"steps_to_gate"`
	PendingLPR  []learnProposal     `json:"pending_lpr,omitempty"`
	Learning    []curatorEffectView `json:"learning,omitempty"`
}

func cmdCurator(argv []string) int {
	if len(argv) == 0 {
		fmt.Fprintln(os.Stderr, "usage: air-worker curator patrol|digest -product <root> [-json]")
		return 2
	}
	switch argv[0] {
	case "patrol":
		return cmdCuratorSnapshot(argv[1:], false)
	case "digest":
		return cmdCuratorSnapshot(argv[1:], true)
	default:
		fmt.Fprintf(os.Stderr, "unknown curator action %q\n", argv[0])
		return 2
	}
}

func curatorPlanPath(root string) (string, error) {
	var cfg runConfig
	cfgPath := filepath.Join(root, "run-config.json")
	if err := readJSON(cfgPath, &cfg); err != nil {
		return "", fmt.Errorf("read run-config.json: %w", err)
	}
	name := strings.TrimSpace(cfg.Plan)
	if name == "" {
		name = "PLAN.md"
	}
	path := filepath.Join(root, name)
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return "", fmt.Errorf("plan not found: %s", path)
	}
	return path, nil
}

func curatorLearningEffects(paths learnPathsSet, proposals []learnProposal) ([]curatorEffectView, error) {
	journal, err := readLearnJournal(paths.Journal)
	if err != nil {
		return nil, err
	}
	var effects []curatorEffectView
	for _, proposal := range proposals {
		if proposal.Status != learnApplied || proposal.AppliedAt == "" {
			continue
		}
		appliedAt, err := time.Parse(time.RFC3339Nano, proposal.AppliedAt)
		if err != nil {
			continue
		}
		repeats := 0
		for _, row := range journal {
			if row.Class != proposal.Class {
				continue
			}
			ts, parseErr := time.Parse(time.RFC3339Nano, row.CreatedAt)
			if parseErr == nil && ts.After(appliedAt) {
				repeats++
			}
		}
		verdict := "сработало"
		if repeats > 0 {
			verdict = "не сработало"
		}
		effects = append(effects, curatorEffectView{
			ProposalID: proposal.ID, Class: proposal.Class, AppliedAt: proposal.AppliedAt,
			RepeatsAfter: repeats, Verdict: verdict,
		})
	}
	return effects, nil
}

func buildCuratorSnapshot(root string) (curatorSnapshot, error) {
	planPath, err := curatorPlanPath(root)
	if err != nil {
		return curatorSnapshot{}, err
	}
	steps := readPlanSteps(planPath)
	snapshot := curatorSnapshot{
		Schema: curatorSchemaVersion, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Product: root, Plan: planPath,
	}
	gateSeen := false
	for _, step := range steps {
		if step.Done {
			continue
		}
		if step.Gate {
			if snapshot.NextGate == nil {
				view := curatorStepView{Num: step.Num, Title: step.Title, Tier: step.Tier, Judge: step.Judge, Gate: true}
				snapshot.NextGate = &view
				gateSeen = true
			}
			continue
		}
		snapshot.OpenSteps++
		if snapshot.NextStep == nil {
			view := curatorStepView{Num: step.Num, Title: step.Title, Tier: step.Tier, Judge: step.Judge}
			snapshot.NextStep = &view
		}
		if !gateSeen {
			snapshot.StepsToGate++
		}
	}

	paths := learnPaths(root)
	proposals, err := readLearnProposals(paths.Proposals)
	if err != nil {
		return curatorSnapshot{}, err
	}
	for _, proposal := range proposals {
		if proposal.Status == learnPending {
			snapshot.PendingLPR = append(snapshot.PendingLPR, proposal)
		}
	}
	snapshot.Learning, err = curatorLearningEffects(paths, proposals)
	if err != nil {
		return curatorSnapshot{}, err
	}
	return snapshot, nil
}

func printCuratorPatrol(snapshot curatorSnapshot) {
	if snapshot.NextStep != nil {
		fmt.Printf("Веха: до ближайшего гейта %d исполняемых шагов; следующий %s | %s | %s\n",
			snapshot.StepsToGate, snapshot.NextStep.Num, snapshot.NextStep.Title, snapshot.NextStep.Tier)
	} else if snapshot.NextGate != nil {
		fmt.Printf("Веха: исполняемых шагов до гейта нет; следующий гейт %s | %s\n",
			snapshot.NextGate.Num, snapshot.NextGate.Title)
	} else {
		fmt.Println("Веха: открытых исполняемых шагов и гейтов нет")
	}
	if snapshot.NextGate != nil {
		fmt.Printf("Гейт: %s | %s\n", snapshot.NextGate.Num, snapshot.NextGate.Title)
	}
	fmt.Printf("ЖДЁТ ДА: %d\n", len(snapshot.PendingLPR))
	for _, proposal := range snapshot.PendingLPR {
		fmt.Printf("  %s | %s | %s | ответ: да %s\n", proposal.ID, proposal.Class, proposal.Rule, proposal.ID)
	}
}

func printCuratorDigest(snapshot curatorSnapshot) {
	fmt.Println("# AirCurator digest")
	fmt.Println()
	fmt.Printf("- generated_utc: %s\n", snapshot.GeneratedAt)
	fmt.Printf("- product: %s\n", snapshot.Product)
	fmt.Printf("- plan: %s\n", snapshot.Plan)
	fmt.Printf("- open executable steps: %d\n", snapshot.OpenSteps)
	fmt.Printf("- steps to nearest open gate: %d\n", snapshot.StepsToGate)
	if snapshot.NextStep != nil {
		fmt.Printf("- next: %s | %s | tier=%s | judge=%s\n",
			snapshot.NextStep.Num, snapshot.NextStep.Title, snapshot.NextStep.Tier, snapshot.NextStep.Judge)
	}
	if snapshot.NextGate != nil {
		fmt.Printf("- gate: %s | %s | judge=%s\n", snapshot.NextGate.Num, snapshot.NextGate.Title, snapshot.NextGate.Judge)
	}
	fmt.Println()
	fmt.Println("## Ждёт да")
	fmt.Println()
	if len(snapshot.PendingLPR) == 0 {
		fmt.Println("Очередь пуста.")
	} else {
		fmt.Println("| id | класс | предложение | как ответить |")
		fmt.Println("|---|---|---|---|")
		for _, proposal := range snapshot.PendingLPR {
			fmt.Printf("| %s | %s | %s | да %s |\n", proposal.ID, proposal.Class, proposal.Rule, proposal.ID)
		}
	}
	fmt.Println()
	fmt.Println("## Самообучение")
	fmt.Println()
	if len(snapshot.Learning) == 0 {
		fmt.Println("Применённых через петлю правил пока нет.")
	} else {
		fmt.Println("| proposal | класс | повторов после | вердикт |")
		fmt.Println("|---|---|---:|---|")
		for _, effect := range snapshot.Learning {
			fmt.Printf("| %s | %s | %d | %s |\n", effect.ProposalID, effect.Class, effect.RepeatsAfter, effect.Verdict)
		}
	}
}

func cmdCuratorSnapshot(argv []string, digest bool) int {
	name := "curator patrol"
	if digest {
		name = "curator digest"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	product := fs.String("product", "", "managed product root")
	all := fs.Bool("all", false, "ecosystem digest from air-worker.products/v1 registry")
	registry := fs.String("registry", "", "portfolio registry path")
	asJSON := fs.Bool("json", false, "machine-readable output")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if *all {
		return cmdCuratorPortfolio(*registry, *asJSON)
	}
	root, err := normalizeLearnProduct(*product)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	snapshot, err := buildCuratorSnapshot(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, name+":", err)
		return 2
	}
	if *asJSON {
		b, err := json.Marshal(snapshot)
		if err != nil {
			fmt.Fprintln(os.Stderr, name+":", err)
			return 2
		}
		fmt.Println(string(b))
		return 0
	}
	if digest {
		printCuratorDigest(snapshot)
	} else {
		printCuratorPatrol(snapshot)
	}
	return 0
}
