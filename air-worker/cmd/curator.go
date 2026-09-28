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
	Schema        string              `json:"schema"`
	GeneratedAt   string              `json:"generated_at"`
	Product       string              `json:"product"`
	Plan          string              `json:"plan"`
	OpenSteps     int                 `json:"open_steps"`
	NextStep      *curatorStepView    `json:"next_step,omitempty"`
	NextGate      *curatorStepView    `json:"next_gate,omitempty"`
	StepsToGate   int                 `json:"steps_to_gate"`
	PendingLPR    []learnProposal     `json:"pending_lpr,omitempty"`
	Learning      []curatorEffectView `json:"learning,omitempty"`
	Peers         []curatorPeer       `json:"peers,omitempty"`
	Assignments   []curatorAssignment `json:"assignments,omitempty"`
	DecisionCount int                 `json:"decision_count,omitempty"`
	Wake          *curatorWakeCard    `json:"wake,omitempty"`
}

func cmdCurator(argv []string) int {
	if len(argv) == 0 {
		fmt.Fprintln(os.Stderr, "usage: air-worker curator patrol|digest|tick|peer|assignment|decision|wake ...")
		return 2
	}
	switch argv[0] {
	case "patrol":
		return cmdCuratorSnapshot(argv[1:], false)
	case "digest":
		return cmdCuratorSnapshot(argv[1:], true)
	case "tick":
		return cmdCuratorTick(argv[1:])
	case "peer":
		return cmdCuratorPeer(argv[1:])
	case "assignment":
		return cmdCuratorAssignment(argv[1:])
	case "decision":
		return cmdCuratorDecision(argv[1:])
	case "wake":
		return cmdCuratorWake(argv[1:])
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

func buildCuratorSnapshotWithControl(root, stateDir string, now time.Time) (curatorSnapshot, error) {
	snapshot, err := buildCuratorSnapshot(root)
	if err != nil {
		return curatorSnapshot{}, err
	}
	snapshot.GeneratedAt = now.UTC().Format(time.RFC3339Nano)

	controlDir := curatorControlDir(stateDir)
	control, err := readCuratorControl(controlDir)
	if err != nil {
		return curatorSnapshot{}, fmt.Errorf("curator control: %w", err)
	}
	snapshot.Peers = append(snapshot.Peers, control.Peers...)
	snapshot.Assignments = append(snapshot.Assignments, control.Assignments...)

	decisions, err := readCuratorDecisions(curatorDecisionPath(controlDir))
	if err != nil {
		return curatorSnapshot{}, fmt.Errorf("curator decisions: %w", err)
	}
	if err := verifyCuratorDecisions(decisions); err != nil {
		return curatorSnapshot{}, fmt.Errorf("curator decisions: %w", err)
	}
	snapshot.DecisionCount = len(decisions)

	statePath := orchestrationWakeStatePath(root)
	var wakeState orchestrationWakeState
	if err := readJSON(statePath, &wakeState); err != nil {
		if !os.IsNotExist(err) {
			return curatorSnapshot{}, fmt.Errorf("curator wake state: %w", err)
		}
	} else {
		card, err := deriveWakeCard(root, statePath, wakeState, now)
		if err != nil {
			return curatorSnapshot{}, fmt.Errorf("curator wake: %w", err)
		}
		snapshot.Wake = &card
	}
	return snapshot, nil
}

func activeCuratorAssignmentCount(assignments []curatorAssignment) int {
	count := 0
	for _, assignment := range assignments {
		if curatorAssignmentActive(assignment) {
			count++
		}
	}
	return count
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
	fmt.Printf("Curator control: peers=%d assignments=%d active=%d decisions=%d\n",
		len(snapshot.Peers), len(snapshot.Assignments), activeCuratorAssignmentCount(snapshot.Assignments), snapshot.DecisionCount)
	if snapshot.Wake != nil {
		fmt.Printf("Wake: target=%s ready=%t action=%s reason=%s\n",
			snapshot.Wake.WakeTarget, snapshot.Wake.Ready, snapshot.Wake.Action, snapshot.Wake.Reason)
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
	fmt.Println("## Curator control")
	fmt.Println()
	fmt.Printf("- peers: %d\n", len(snapshot.Peers))
	fmt.Printf("- assignments: %d (active/paused/degraded: %d)\n", len(snapshot.Assignments), activeCuratorAssignmentCount(snapshot.Assignments))
	fmt.Printf("- audit decisions: %d\n", snapshot.DecisionCount)
	if snapshot.Wake == nil {
		fmt.Println("- wake: no orchestration state")
	} else {
		fmt.Printf("- wake: target=%s ready=%t action=%s reason=%s\n",
			snapshot.Wake.WakeTarget, snapshot.Wake.Ready, snapshot.Wake.Action, snapshot.Wake.Reason)
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
	stateDir := fs.String("state-dir", "", "override machine curator state dir")
	nowRaw := fs.String("now", "", "override current time RFC3339 for deterministic wake calculation")
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
	now := time.Now().UTC()
	if strings.TrimSpace(*nowRaw) != "" {
		now, err = time.Parse(time.RFC3339, strings.TrimSpace(*nowRaw))
		if err != nil {
			fmt.Fprintln(os.Stderr, name+" -now:", err)
			return 2
		}
	}
	snapshot, err := buildCuratorSnapshotWithControl(root, *stateDir, now)
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
