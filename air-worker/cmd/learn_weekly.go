package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	learnRuleStaleAfter   = 14 * 24 * time.Hour
	learnRuleArchiveAfter = 30 * 24 * time.Hour
)

type learnWeeklyTransition struct {
	ProposalID string `json:"proposal_id"`
	From       string `json:"from"`
	To         string `json:"to"`
	IdleHours  int64  `json:"idle_hours"`
}

type learnWeeklyRun struct {
	Schema         string                  `json:"schema"`
	RunID          string                  `json:"run_id"`
	Product        string                  `json:"product"`
	StartedAt      string                  `json:"started_at"`
	FinishedAt     string                  `json:"finished_at"`
	Snapshot       string                  `json:"snapshot"`
	Transitions    []learnWeeklyTransition `json:"transitions"`
	MergeGroups    map[string][]string     `json:"merge_groups,omitempty"`
	MergeProposals []string                `json:"merge_proposals,omitempty"`
	MergeReviewErr string                  `json:"merge_review_error,omitempty"`
	Report         string                  `json:"report"`
}

type learnWeeklySnapshot struct {
	Schema    string               `json:"schema"`
	CreatedAt string               `json:"created_at"`
	Rules     []learnRuleRecord    `json:"rules"`
	Usage     []learnRuleUsageStat `json:"usage"`
}

var weeklyMergeReviewModel = learnReviewModel

func learnWeeklyDir(product string) string {
	return filepath.Join(learnPaths(product).Root, "weekly")
}

func writeLearnWeeklySnapshot(runDir string, now time.Time, rules []learnRuleRecord, usage []learnRuleUsageStat) (string, error) {
	snapshot := learnWeeklySnapshot{
		Schema: "air-worker.learn.weekly.snapshot/v1", CreatedAt: now.UTC().Format(time.RFC3339Nano),
		Rules: append([]learnRuleRecord(nil), rules...), Usage: append([]learnRuleUsageStat(nil), usage...),
	}
	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(runDir, "snapshot.json")
	if err := writeLearnAtomic(path, append(raw, '\n')); err != nil {
		return "", err
	}
	return path, nil
}

func learnWeeklyMergeGroups(rules []learnRuleRecord) map[string][]string {
	groups := map[string][]string{}
	for _, rule := range rules {
		if rule.Status == learnRuleArchived || strings.TrimSpace(rule.Class) == "" {
			continue
		}
		groups[rule.Class] = append(groups[rule.Class], rule.ProposalID)
	}
	for class, ids := range groups {
		if len(ids) < 2 {
			delete(groups, class)
			continue
		}
		sort.Strings(ids)
		groups[class] = ids
	}
	return groups
}

func buildWeeklyMergePacket(rules []learnRuleRecord, groups map[string][]string) []byte {
	var b strings.Builder
	b.WriteString("# AirWorker weekly merge review\n\n")
	b.WriteString("This is a proposal-only review. Never apply, revoke, weaken, or edit active rules.\n")
	b.WriteString("For duplicate or narrow rules below, propose one more general durable rule only when semantics can be preserved.\n")
	b.WriteString("Every proposal must include trigger, check_type, machine-checkable check_spec and a violation -> block testcase.\n\n")
	for _, rule := range rules {
		if _, ok := groups[rule.Class]; !ok {
			continue
		}
		fmt.Fprintf(&b, "- %s class=%s status=%s trigger=%s check=%s/%s :: %s\n",
			rule.ProposalID, rule.Class, rule.Status, rule.Trigger, rule.CheckType, rule.CheckSpec, rule.Rule)
	}
	return []byte(b.String())
}

func appendWeeklyMergeProposals(product, runID string, candidates []learnReviewCandidate, existing []learnProposal, now time.Time) ([]string, error) {
	paths := learnPaths(product)
	var ids []string
	for _, candidate := range candidates {
		class, classErr := cleanLearnText("class", candidate.Class)
		rule, ruleErr := cleanLearnText("rule", candidate.Rule)
		if classErr != nil || ruleErr != nil {
			continue
		}
		duplicate := false
		for _, row := range existing {
			if row.Status != learnRevoked && strings.EqualFold(row.Class, class) && strings.EqualFold(row.Rule, rule) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		id, err := newLearnID("LP", now)
		if err != nil {
			return ids, err
		}
		row := learnProposal{
			Schema: learnSchemaVersion, ID: id, CreatedAt: now.UTC().Format(time.RFC3339Nano),
			Status: learnPending, Class: class, Rule: rule,
			Trigger: strings.TrimSpace(candidate.Trigger), CheckType: strings.ToLower(strings.TrimSpace(candidate.CheckType)),
			CheckSpec: strings.TrimSpace(candidate.CheckSpec), TestCase: strings.TrimSpace(candidate.TestCase),
			SourceIDs: []string{"weekly:" + runID},
		}
		if err := validateLearnProposalSpec(row); err != nil {
			continue
		}
		if err := appendLearnJSON(paths.Proposals, row); err != nil {
			return ids, err
		}
		existing = append(existing, row)
		ids = append(ids, id)
	}
	return ids, nil
}

func runWeeklyMergeReview(product, runID, runDir string, rules []learnRuleRecord, groups map[string][]string, now time.Time) ([]string, error) {
	if len(groups) == 0 {
		return nil, nil
	}
	packetPath := filepath.Join(runDir, ".merge-review.packet.md")
	if err := os.WriteFile(packetPath, buildWeeklyMergePacket(rules, groups), 0o600); err != nil {
		return nil, err
	}
	defer os.Remove(packetPath)
	candidates, err := weeklyMergeReviewModel(product, packetPath)
	if err != nil {
		return nil, err
	}
	existing, err := readLearnProposals(learnPaths(product).Proposals)
	if err != nil {
		return nil, err
	}
	return appendWeeklyMergeProposals(product, runID, candidates, existing, now)
}

func weeklyTransitionTarget(status string, idle time.Duration) string {
	switch {
	case idle >= learnRuleArchiveAfter:
		return learnRuleArchived
	case idle >= learnRuleStaleAfter && status == learnApplied:
		return learnRuleStale
	default:
		return status
	}
}

func writeWeeklyRuleTransition(product string, rule learnRuleRecord, target string, now time.Time) error {
	if target == rule.Status {
		return nil
	}
	rule.Status = target
	raw, err := json.MarshalIndent(rule, "", "  ")
	if err != nil {
		return err
	}
	if err := writeLearnAtomic(filepath.Join(learnPaths(product).RulesDir, rule.ProposalID+".json"), append(raw, '\n')); err != nil {
		return err
	}
	id, err := newLearnID("LR", now)
	if err != nil {
		return err
	}
	event := learnJournalRecord{
		Schema: learnSchemaVersion, ID: id, CreatedAt: now.UTC().Format(time.RFC3339Nano),
		Class: rule.Class, Observed: fmt.Sprintf("rule %s lifecycle -> %s", rule.ProposalID, target),
		Evidence: "weekly maintenance; no rule file deleted", Kind: "check", Source: "air-worker-weekly",
		Actor: "core", Reference: rule.ProposalID,
	}
	if err := validateLearnEvent(event); err != nil {
		return err
	}
	return appendLearnJSON(learnPaths(product).Journal, event)
}

func renderWeeklyReport(run learnWeeklyRun) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# AirWorker weekly learning report\n\nRun: %s\nProduct: %s\nStarted: %s\n\n", run.RunID, run.Product, run.StartedAt)
	fmt.Fprintf(&b, "## Lifecycle\n\nTransitions: %d\n", len(run.Transitions))
	for _, tr := range run.Transitions {
		fmt.Fprintf(&b, "- %s: %s -> %s (idle %dh)\n", tr.ProposalID, tr.From, tr.To, tr.IdleHours)
	}
	fmt.Fprintf(&b, "\n## Merge review\n\nCandidate groups: %d\n", len(run.MergeGroups))
	classes := make([]string, 0, len(run.MergeGroups))
	for class := range run.MergeGroups {
		classes = append(classes, class)
	}
	sort.Strings(classes)
	for _, class := range classes {
		fmt.Fprintf(&b, "- %s: %s\n", class, strings.Join(run.MergeGroups[class], ", "))
	}
	fmt.Fprintf(&b, "Pending merge proposals: %d\n", len(run.MergeProposals))
	if run.MergeReviewErr != "" {
		fmt.Fprintf(&b, "Merge review error: %s\n", run.MergeReviewErr)
	}
	fmt.Fprintf(&b, "\nSnapshot: %s\n", run.Snapshot)
	return []byte(b.String())
}

func runLearnWeekly(product string, now time.Time) (learnWeeklyRun, error) {
	root, err := normalizeLearnProduct(product)
	if err != nil {
		return learnWeeklyRun{}, err
	}
	runID, err := newLearnID("LW", now.UTC())
	if err != nil {
		return learnWeeklyRun{}, err
	}
	runDir := filepath.Join(learnWeeklyDir(root), runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return learnWeeklyRun{}, err
	}
	rules, _, err := readLearnRuleRecords(learnPaths(root))
	if err != nil {
		return learnWeeklyRun{}, err
	}
	usage, err := collectLearnRuleUsage(root, now)
	if err != nil {
		return learnWeeklyRun{}, err
	}
	snapshotPath, err := writeLearnWeeklySnapshot(runDir, now, rules, usage)
	if err != nil {
		return learnWeeklyRun{}, err
	}
	run := learnWeeklyRun{
		Schema: "air-worker.learn.weekly/v1", RunID: runID, Product: root,
		StartedAt:   now.UTC().Format(time.RFC3339Nano),
		Snapshot:    filepath.ToSlash(snapshotPath),
		MergeGroups: learnWeeklyMergeGroups(rules),
	}
	usageByID := map[string]learnRuleUsageStat{}
	for _, stat := range usage {
		usageByID[stat.ProposalID] = stat
	}
	for _, rule := range rules {
		stat := usageByID[rule.ProposalID]
		last, parseErr := time.Parse(time.RFC3339Nano, stat.LastActivity)
		if parseErr != nil {
			continue
		}
		idle := now.Sub(last)
		target := weeklyTransitionTarget(rule.Status, idle)
		if target == rule.Status {
			continue
		}
		if err := writeWeeklyRuleTransition(root, rule, target, now); err != nil {
			return run, err
		}
		run.Transitions = append(run.Transitions, learnWeeklyTransition{
			ProposalID: rule.ProposalID, From: rule.Status, To: target, IdleHours: int64(idle / time.Hour),
		})
	}
	if ids, mergeErr := runWeeklyMergeReview(root, runID, runDir, rules, run.MergeGroups, now); mergeErr != nil {
		run.MergeReviewErr = mergeErr.Error()
	} else {
		run.MergeProposals = ids
	}
	run.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	run.Report = filepath.ToSlash(filepath.Join(runDir, "REPORT.md"))
	if err := writeLearnAtomic(filepath.Join(runDir, "REPORT.md"), renderWeeklyReport(run)); err != nil {
		return run, err
	}
	raw, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return run, err
	}
	if err := writeLearnAtomic(filepath.Join(runDir, "run.json"), append(raw, '\n')); err != nil {
		return run, err
	}
	return run, nil
}

func cmdLearnWeekly(argv []string) int {
	fs := flag.NewFlagSet("learn weekly", flag.ContinueOnError)
	product := fs.String("product", "", "managed product root")
	nowRaw := fs.String("now", "", "override current time RFC3339")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	now := time.Now().UTC()
	var err error
	if strings.TrimSpace(*nowRaw) != "" {
		now, err = time.Parse(time.RFC3339, strings.TrimSpace(*nowRaw))
		if err != nil {
			fmt.Fprintln(os.Stderr, "learn weekly -now:", err)
			return 2
		}
	}
	run, err := runLearnWeekly(*product, now)
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn weekly:", err)
		return 2
	}
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(run)
	} else {
		fmt.Printf("%s transitions=%d merge_proposals=%d report=%s%s",
			run.RunID, len(run.Transitions), len(run.MergeProposals), run.Report, lineEnding)
	}
	if run.MergeReviewErr != "" {
		return 3
	}
	return 0
}
