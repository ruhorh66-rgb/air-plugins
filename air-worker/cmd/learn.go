package main

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	learnSchemaVersion = "air-worker.learn/v1"
	learnPending       = "PENDING_LPR"
	learnApplied       = "APPLIED"
	learnRevoked       = "REVOKED"
	learnRuleStale     = "STALE"
	learnRuleArchived  = "ARCHIVED"
	learnRolledBack    = "ROLLED_BACK" // accepted on read from 0.10
	learnRulesHeader   = "# AirWorker learned rules\n\n<!-- Active only after explicit LPR approval: да <proposal-id>. -->\n"
)

type learnJournalRecord struct {
	Schema          string `json:"schema"`
	ID              string `json:"id"`
	CreatedAt       string `json:"created_at"`
	Class           string `json:"class"`
	Observed        string `json:"observed"`
	Evidence        string `json:"evidence,omitempty"`
	Kind            string `json:"kind"`
	Source          string `json:"source"`
	Actor           string `json:"actor,omitempty"`
	Reference       string `json:"reference,omitempty"`
	ImportID        string `json:"import_id,omitempty"`
	SourceTimestamp string `json:"source_timestamp,omitempty"`
}

type learnProposal struct {
	Schema     string   `json:"schema"`
	ID         string   `json:"id"`
	CreatedAt  string   `json:"created_at"`
	Status     string   `json:"status"`
	Class      string   `json:"class"`
	Rule       string   `json:"rule"`
	Trigger    string   `json:"trigger,omitempty"`
	CheckType  string   `json:"check_type,omitempty"`
	CheckSpec  string   `json:"check_spec,omitempty"`
	TestCase   string   `json:"test_case,omitempty"`
	SourceIDs  []string `json:"source_ids,omitempty"`
	AppliedAt  string   `json:"applied_at,omitempty"`
	ApprovedAt string   `json:"approved_at,omitempty"`
	LedgerID   string   `json:"ledger_id,omitempty"`
	Approval   string   `json:"approval,omitempty"`
}

type learnLedgerRecord struct {
	Schema         string `json:"schema"`
	ID             string `json:"id"`
	CreatedAt      string `json:"created_at"`
	Action         string `json:"action"`
	ProposalID     string `json:"proposal_id,omitempty"`
	Class          string `json:"class,omitempty"`
	RulePath       string `json:"rule_path"`
	BeforeExists   bool   `json:"before_exists"`
	BeforeSHA256   string `json:"before_sha256,omitempty"`
	AfterSHA256    string `json:"after_sha256,omitempty"`
	BeforeBlob     string `json:"before_blob,omitempty"`
	AfterBlob      string `json:"after_blob,omitempty"`
	GitHead        string `json:"git_head,omitempty"`
	Approval       string `json:"approval,omitempty"`
	TargetLedgerID string `json:"target_ledger_id,omitempty"`
}

type learnPathsSet struct {
	Root      string
	Durable   string
	Rules     string
	RulesDir  string
	Journal   string
	Proposals string
	Ledger    string
	Blobs     string
}

func learnPaths(product string) learnPathsSet {
	root := filepath.Join(product, ".air-worker", "learn")
	durable := filepath.Join(product, "learn")
	return learnPathsSet{
		Root: root, Durable: durable, Rules: filepath.Join(root, "RULES.md"),
		RulesDir:  filepath.Join(durable, "rules"),
		Journal:   filepath.Join(durable, "events.jsonl"),
		Proposals: filepath.Join(durable, "proposals.jsonl"),
		Ledger:    filepath.Join(root, "ledger.jsonl"),
		Blobs:     filepath.Join(root, "blobs"),
	}
}

func normalizeLearnProduct(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("-product is required")
	}
	root, err := filepath.Abs(raw)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		return "", fmt.Errorf("product directory not found: %s", root)
	}
	return root, nil
}

func cleanLearnText(name, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("-%s is required", name)
	}
	value = strings.Join(strings.Fields(value), " ")
	if len([]rune(value)) > 2000 {
		return "", fmt.Errorf("-%s is too long", name)
	}
	return value, nil
}

func newLearnID(prefix string, now time.Time) (string, error) {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s-%s", prefix, now.UTC().Format("20060102T150405Z"), hex.EncodeToString(suffix[:])), nil
}

func appendLearnJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

func readLearnJournal(path string) ([]learnJournalRecord, error) {
	product := filepath.Dir(filepath.Dir(filepath.Clean(path)))
	if err := migrateProductJournal(product); err != nil {
		return nil, err
	}
	return readLearnEventsNoMigration(path)
}

func readLearnProposals(path string) ([]learnProposal, error) {
	product := filepath.Dir(filepath.Dir(filepath.Clean(path)))
	if err := migrateLegacyProposals(path, filepath.Join(product, ".air-worker", "learn", "proposals.jsonl")); err != nil {
		return nil, err
	}
	return readLearnProposalsNoMigration(path)
}

func readLearnProposalsNoMigration(path string) ([]learnProposal, error) {
	var out []learnProposal
	err := scanLearnJSONL(path, func(b []byte) error {
		var row learnProposal
		if err := json.Unmarshal(b, &row); err != nil {
			return err
		}
		if row.Status == learnRolledBack {
			row.Status = learnRevoked
		}
		out = append(out, row)
		return nil
	})
	return out, err
}

func migrateLegacyProposals(target, legacy string) error {
	info, err := os.Stat(legacy)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("legacy proposals is not a regular file: %s", legacy)
	}
	canonical, err := readLearnProposalsNoMigration(target)
	if err != nil {
		return err
	}
	old, err := readLearnProposalsNoMigration(legacy)
	if err != nil {
		return err
	}
	byID := make(map[string]learnProposal, len(canonical))
	for _, row := range canonical {
		if previous, ok := byID[row.ID]; ok && !sameLearnProposal(previous, row) {
			return fmt.Errorf("canonical proposal id conflict: %s", row.ID)
		}
		byID[row.ID] = row
	}
	added := false
	for _, row := range old {
		if previous, ok := byID[row.ID]; ok {
			if !sameLearnProposal(previous, row) && !learnProposalSupersedes(previous, row) {
				return fmt.Errorf("legacy/canonical proposal id conflict: %s", row.ID)
			}
			continue
		}
		canonical = append(canonical, row)
		byID[row.ID] = row
		added = true
	}
	if !added {
		return nil
	}
	b, err := marshalLearnJSONL(canonical)
	if err != nil {
		return err
	}
	return writeLearnAtomic(target, b)
}

func sameLearnProposal(a, b learnProposal) bool {
	aJSON, _ := json.Marshal(a)
	bJSON, _ := json.Marshal(b)
	return string(aJSON) == string(bJSON)
}

func learnProposalSupersedes(canonical, legacy learnProposal) bool {
	if canonical.Schema != legacy.Schema || learnProposalSHA(canonical) != learnProposalSHA(legacy) {
		return false
	}
	rank := map[string]int{learnPending: 1, learnApplied: 2, learnRevoked: 3}
	return rank[canonical.Status] > rank[legacy.Status]
}

func readLearnLedger(path string) ([]learnLedgerRecord, error) {
	var out []learnLedgerRecord
	err := scanLearnJSONL(path, func(b []byte) error {
		var row learnLedgerRecord
		if err := json.Unmarshal(b, &row); err != nil {
			return err
		}
		out = append(out, row)
		return nil
	})
	return out, err
}

func scanLearnJSONL(path string, accept func([]byte) error) error {
	return scanLearnJSONLIndexed(path, func(_ int, raw []byte) error { return accept(raw) })
}

func scanLearnJSONLIndexed(path string, accept func(int, []byte) error) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64*1024), 2*1024*1024)
	line := 0
	for s.Scan() {
		line++
		raw := append([]byte(nil), s.Bytes()...)
		if strings.TrimSpace(string(raw)) == "" {
			continue
		}
		if err := accept(line, raw); err != nil {
			return fmt.Errorf("%s:%d: %w", path, line, err)
		}
	}
	return s.Err()
}

func marshalLearnJSONL[T any](rows []T) ([]byte, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	for _, row := range rows {
		if err := enc.Encode(row); err != nil {
			return nil, err
		}
	}
	return []byte(b.String()), nil
}

func writeLearnAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

func learnSHA(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

var errLearnRulesUnapproved = errors.New("active RULES.md has no matching apply/rollback ledger evidence")

func saveLearnBlob(paths learnPathsSet, data []byte) (string, error) {
	if err := os.MkdirAll(paths.Blobs, 0o755); err != nil {
		return "", err
	}
	sha := learnSHA(data)
	path := filepath.Join(paths.Blobs, sha+".blob")
	if _, err := os.Stat(path); err == nil {
		return filepath.ToSlash(filepath.Join("blobs", sha+".blob")), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Join("blobs", sha+".blob")), nil
}

func learnGitHead(product string) string {
	cmd := exec.Command("git", "-C", product, "rev-parse", "HEAD")
	b, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func findLearnProposal(rows []learnProposal, id string) (int, *learnProposal) {
	for i := range rows {
		if rows[i].ID == id {
			return i, &rows[i]
		}
	}
	return -1, nil
}

func findLearnLedger(rows []learnLedgerRecord, id string) *learnLedgerRecord {
	for i := range rows {
		if rows[i].ID == id {
			return &rows[i]
		}
	}
	return nil
}

func cmdLearn(argv []string) int {
	if len(argv) == 0 {
		fmt.Fprintln(os.Stderr, "usage: air-worker learn add|event|migrate-legacy|propose|pending|apply|effect|usage|weekly|context|rollback ...")
		return 2
	}
	switch argv[0] {
	case "add":
		return cmdLearnAdd(argv[1:])
	case "event":
		return cmdLearnEvent(argv[1:])
	case "migrate-legacy":
		return cmdLearnMigrateLegacy(argv[1:])
	case "propose":
		return cmdLearnPropose(argv[1:])
	case "pending":
		return cmdLearnPending(argv[1:])
	case "apply":
		return cmdLearnApply(argv[1:])
	case "effect":
		return cmdLearnEffect(argv[1:])
	case "usage":
		return cmdLearnUsage(argv[1:])
	case "weekly":
		return cmdLearnWeekly(argv[1:])
	case "context":
		return cmdLearnContext(argv[1:])
	case "rollback":
		return cmdLearnRollback(argv[1:])
	case "review":
		return cmdLearnReview(argv[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown learn action %q\n", argv[0])
		return 2
	}
}

func cmdLearnAdd(argv []string) int {
	return cmdLearnEventWithName("learn add", argv)
}

func cmdLearnEvent(argv []string) int {
	return cmdLearnEventWithName("learn event", argv)
}

func cmdLearnEventWithName(name string, argv []string) int {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	product := fs.String("product", "", "managed product root")
	class := fs.String("class", "", "generalized lesson class")
	observed := fs.String("observed", "", "what happened")
	evidence := fs.String("evidence", "", "optional evidence reference")
	kind := fs.String("kind", "lesson", "lesson, correction, violation, check, or judge_result")
	source := fs.String("source", "worker", "event source")
	actor := fs.String("actor", "", "actor/session name")
	actorKind := fs.String("actor-kind", "", "gpt-window or claude-session")
	reference := fs.String("reference", "", "optional related event or artifact reference")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	root, err := normalizeLearnProduct(*product)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	c, err := cleanLearnText("class", *class)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	o, err := cleanLearnText("observed", *observed)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	resolvedActor, err := resolveMutationActor(*actorKind, *actor, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, name+":", err)
		return 2
	}
	now := time.Now().UTC()
	id, err := newLearnID("LR", now)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	row := learnJournalRecord{Schema: learnSchemaVersion, ID: id, CreatedAt: now.Format(time.RFC3339Nano), Class: c, Observed: o, Evidence: strings.TrimSpace(*evidence), Kind: strings.TrimSpace(*kind), Source: strings.TrimSpace(*source), Actor: resolvedActor, Reference: strings.TrimSpace(*reference)}
	if err := validateLearnEvent(row); err != nil {
		fmt.Fprintln(os.Stderr, name+":", err)
		return 2
	}
	if err := migrateProductJournal(root); err != nil {
		fmt.Fprintln(os.Stderr, name+":", err)
		return 2
	}
	if err := appendLearnJSON(learnPaths(root).Journal, row); err != nil {
		fmt.Fprintln(os.Stderr, name+":", err)
		return 2
	}
	if *asJSON {
		if err := json.NewEncoder(os.Stdout).Encode(row); err != nil {
			fmt.Fprintln(os.Stderr, name+":", err)
			return 2
		}
	} else {
		fmt.Println(id)
	}
	return 0
}

func cmdLearnPropose(argv []string) int {
	fs := flag.NewFlagSet("learn propose", flag.ContinueOnError)
	product := fs.String("product", "", "managed product root")
	class := fs.String("class", "", "generalized lesson class")
	rule := fs.String("rule", "", "proposed active rule")
	trigger := fs.String("trigger", "", "event/condition that should invoke the check")
	checkType := fs.String("check-type", "", "hook, gate, or script")
	checkSpec := fs.String("check-spec", "", "machine-checkable specification")
	testCase := fs.String("test-case", "", "violation -> expected block testcase")
	source := fs.String("source", "", "comma-separated learning record ids")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	root, err := normalizeLearnProduct(*product)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	c, err := cleanLearnText("class", *class)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	r, err := cleanLearnText("rule", *rule)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var sources []string
	for _, item := range strings.Split(*source, ",") {
		if v := strings.TrimSpace(item); v != "" {
			sources = append(sources, v)
		}
	}
	now := time.Now().UTC()
	id, err := newLearnID("LP", now)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	row := learnProposal{
		Schema: learnSchemaVersion, ID: id, CreatedAt: now.Format(time.RFC3339Nano),
		Status: learnPending, Class: c, Rule: r,
		Trigger: strings.TrimSpace(*trigger), CheckType: strings.ToLower(strings.TrimSpace(*checkType)),
		CheckSpec: strings.TrimSpace(*checkSpec), TestCase: strings.TrimSpace(*testCase),
		SourceIDs: sources,
	}
	if err := validateLearnProposalSpec(row); err != nil {
		fmt.Fprintln(os.Stderr, "learn propose:", err)
		return 2
	}
	proposalPath := learnPaths(root).Proposals
	if _, err := readLearnProposals(proposalPath); err != nil {
		fmt.Fprintln(os.Stderr, "learn propose:", err)
		return 2
	}
	if err := appendLearnJSON(proposalPath, row); err != nil {
		fmt.Fprintln(os.Stderr, "learn propose:", err)
		return 2
	}
	fmt.Printf("%s %s\n", id, learnPending)
	return 0
}

func cmdLearnPending(argv []string) int {
	fs := flag.NewFlagSet("learn pending", flag.ContinueOnError)
	product := fs.String("product", "", "managed product root")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	root, err := normalizeLearnProduct(*product)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	rows, err := readLearnProposals(learnPaths(root).Proposals)
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn pending:", err)
		return 2
	}
	pending := make([]learnProposal, 0)
	for _, row := range rows {
		if row.Status == learnPending {
			pending = append(pending, row)
		}
	}
	if *asJSON {
		b, _ := json.Marshal(map[string]any{"schema": learnSchemaVersion, "status": learnPending, "count": len(pending), "proposals": pending})
		fmt.Println(string(b))
		return 0
	}
	if len(pending) == 0 {
		fmt.Println("PENDING_LPR 0")
		return 0
	}
	fmt.Printf("PENDING_LPR %d\n", len(pending))
	for _, row := range pending {
		fmt.Printf("%s | %s | %s\n", row.ID, row.Class, row.Rule)
	}
	return 0
}

func cmdLearnApply(argv []string) int {
	fs := flag.NewFlagSet("learn apply", flag.ContinueOnError)
	product := fs.String("product", "", "managed product root")
	id := fs.String("id", "", "proposal id (or pass it as the first positional argument)")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	root, err := normalizeLearnProduct(*product)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	proposalID := strings.TrimSpace(*id)
	if proposalID == "" && fs.NArg() > 0 {
		proposalID = strings.TrimSpace(fs.Arg(0))
	}
	if proposalID == "" {
		fmt.Fprintln(os.Stderr, "proposal id is required")
		return 2
	}
	paths := learnPaths(root)
	proposals, err := readLearnProposals(paths.Proposals)
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn apply:", err)
		return 2
	}
	idx, proposal := findLearnProposal(proposals, proposalID)
	if proposal == nil {
		fmt.Fprintln(os.Stderr, "proposal not found:", proposalID)
		return 2
	}
	if proposal.Status != learnPending {
		fmt.Fprintf(os.Stderr, "proposal %s is %s, not %s\n", proposalID, proposal.Status, learnPending)
		return 2
	}
	hasExecutableSpec := strings.TrimSpace(proposal.Trigger) != "" ||
		strings.TrimSpace(proposal.CheckType) != "" ||
		strings.TrimSpace(proposal.CheckSpec) != "" ||
		strings.TrimSpace(proposal.TestCase) != ""
	if hasExecutableSpec {
		if err := validateLearnProposalSpec(*proposal); err != nil {
			fmt.Fprintln(os.Stderr, "learn apply: invalid executable rule spec:", err)
			return 2
		}
		if err := supportsExecutableLearnProposal(*proposal); err != nil {
			fmt.Fprintln(os.Stderr, "learn apply: executable rule is not implemented:", err)
			return 2
		}
	}
	grant, grantClaimPath, grantPath, err := claimLearnApprovalGrant(root, *proposal)
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn apply: approval gate:", err)
		return 3
	}
	mutationCommitted := false
	defer func() {
		if !mutationCommitted {
			releaseLearnApprovalClaim(grantClaimPath, grantPath)
		}
	}()
	if hasExecutableSpec {
		committed, applyErr := applyVerifiedExecutableProposal(root, paths, proposals, idx, *proposal, grant, grantClaimPath, grantPath)
		mutationCommitted = committed
		if applyErr != nil {
			fmt.Fprintln(os.Stderr, "learn apply:", applyErr)
			return 2
		}
		return 0
	}
	originalProposals, err := os.ReadFile(paths.Proposals)
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn apply:", err)
		return 2
	}
	before, readErr := os.ReadFile(paths.Rules)
	beforeExists := readErr == nil
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		fmt.Fprintln(os.Stderr, "learn apply:", readErr)
		return 2
	}
	base := before
	if !beforeExists {
		base = []byte(learnRulesHeader)
	}
	ruleLine := fmt.Sprintf("- [%s] class=%s :: %s\n", proposal.ID, proposal.Class, proposal.Rule)
	after := append(append([]byte(nil), base...), []byte(ruleLine)...)
	beforeBlob := ""
	if beforeExists {
		beforeBlob, err = saveLearnBlob(paths, before)
		if err != nil {
			fmt.Fprintln(os.Stderr, "learn apply:", err)
			return 2
		}
	}
	afterBlob, err := saveLearnBlob(paths, after)
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn apply:", err)
		return 2
	}
	now := time.Now().UTC()
	ledgerID, err := newLearnID("LM", now)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	ledger := learnLedgerRecord{
		Schema: learnSchemaVersion, ID: ledgerID, CreatedAt: now.Format(time.RFC3339Nano),
		Action: "apply", ProposalID: proposal.ID, Class: proposal.Class,
		RulePath:     filepath.ToSlash(filepath.Join(".air-worker", "learn", "RULES.md")),
		BeforeExists: beforeExists, AfterSHA256: learnSHA(after), AfterBlob: afterBlob,
		GitHead: learnGitHead(root), Approval: "grant:" + grant.ID,
	}
	if beforeExists {
		ledger.BeforeSHA256 = learnSHA(before)
		ledger.BeforeBlob = beforeBlob
	}
	proposals[idx].Status = learnApplied
	proposals[idx].AppliedAt = now.Format(time.RFC3339Nano)
	proposals[idx].ApprovedAt = now.Format(time.RFC3339Nano)
	proposals[idx].LedgerID = ledgerID
	proposals[idx].Approval = "grant:" + grant.ID
	proposalBytes, err := marshalLearnJSONL(proposals)
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn apply:", err)
		return 2
	}
	if err := writeLearnAtomic(paths.Rules, after); err != nil {
		fmt.Fprintln(os.Stderr, "learn apply:", err)
		return 2
	}
	rollbackRules := func() {
		if beforeExists {
			_ = writeLearnAtomic(paths.Rules, before)
		} else {
			_ = os.Remove(paths.Rules)
		}
	}
	if err := writeLearnAtomic(paths.Proposals, proposalBytes); err != nil {
		rollbackRules()
		fmt.Fprintln(os.Stderr, "learn apply: proposal transaction failed:", err)
		return 2
	}
	if err := appendLearnJSON(paths.Ledger, ledger); err != nil {
		rollbackRules()
		_ = writeLearnAtomic(paths.Proposals, originalProposals)
		fmt.Fprintln(os.Stderr, "learn apply: ledger transaction failed; mutation rolled back:", err)
		return 2
	}
	// From this point the learning mutation is committed. Never release an
	// unconsumed claim back into the approval pool if final bookkeeping fails.
	mutationCommitted = true
	if err := consumeLearnApprovalGrant(grantClaimPath, grantPath, grant, ledger.ID); err != nil {
		fmt.Fprintln(os.Stderr, "learn apply: rule applied and ledgered, but approval grant could not be published as consumed; claim stays unavailable:", err)
		return 2
	}
	fmt.Printf("%s APPLIED ledger=%s grant=%s before=%s after=%s\n", proposal.ID, ledger.ID, grant.ID, ledger.BeforeSHA256, ledger.AfterSHA256)
	return 0
}

func cmdLearnEffect(argv []string) int {
	fs := flag.NewFlagSet("learn effect", flag.ContinueOnError)
	product := fs.String("product", "", "managed product root")
	id := fs.String("id", "", "applied proposal id")
	class := fs.String("class", "", "class if proposal id is omitted")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	root, err := normalizeLearnProduct(*product)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	paths := learnPaths(root)
	var targetClass, appliedAt, proposalID string
	if strings.TrimSpace(*id) != "" {
		rows, err := readLearnProposals(paths.Proposals)
		if err != nil {
			fmt.Fprintln(os.Stderr, "learn effect:", err)
			return 2
		}
		_, p := findLearnProposal(rows, strings.TrimSpace(*id))
		if p == nil {
			fmt.Fprintln(os.Stderr, "proposal not found:", *id)
			return 2
		}
		if p.Status != learnApplied || p.AppliedAt == "" {
			fmt.Printf("%s | рано | proposal=%s status=%s\n", p.Class, p.ID, p.Status)
			return 0
		}
		targetClass, appliedAt, proposalID = p.Class, p.AppliedAt, p.ID
	} else {
		targetClass, err = cleanLearnText("class", *class)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		proposals, err := readLearnProposals(paths.Proposals)
		if err != nil {
			fmt.Fprintln(os.Stderr, "learn effect:", err)
			return 2
		}
		for i := len(proposals) - 1; i >= 0; i-- {
			if proposals[i].Status == learnApplied && proposals[i].Class == targetClass {
				appliedAt, proposalID = proposals[i].AppliedAt, proposals[i].ID
				break
			}
		}
		if appliedAt == "" {
			fmt.Printf("%s | рано | no applied rule\n", targetClass)
			return 0
		}
	}
	appliedTime, err := time.Parse(time.RFC3339Nano, appliedAt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn effect: invalid applied_at:", err)
		return 2
	}
	journal, err := readLearnJournal(paths.Journal)
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn effect:", err)
		return 2
	}
	repeats := 0
	for _, row := range journal {
		if row.Class != targetClass {
			continue
		}
		t, parseErr := time.Parse(time.RFC3339Nano, row.CreatedAt)
		if parseErr == nil && t.After(appliedTime) {
			repeats++
		}
	}
	verdict := "сработало"
	if repeats > 0 {
		verdict = "не сработало"
	}
	result := map[string]any{"schema": learnSchemaVersion, "proposal_id": proposalID, "class": targetClass, "applied_at": appliedAt, "repeats_after": repeats, "verdict": verdict}
	if *asJSON {
		b, _ := json.Marshal(result)
		fmt.Println(string(b))
	} else {
		fmt.Printf("%s | proposal=%s | повторов после=%d | %s\n", targetClass, proposalID, repeats, verdict)
	}
	return 0
}

func cmdLearnContext(argv []string) int {
	fs := flag.NewFlagSet("learn context", flag.ContinueOnError)
	product := fs.String("product", "", "managed product root")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	root, err := normalizeLearnProduct(*product)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	b, err := approvedLearnRules(root)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn context:", err)
		return 3
	}
	_, _ = os.Stdout.Write(b)
	return 0
}

func cmdLearnRollback(argv []string) int {
	fs := flag.NewFlagSet("learn rollback", flag.ContinueOnError)
	product := fs.String("product", "", "managed product root")
	id := fs.String("id", "", "apply ledger id")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	root, err := normalizeLearnProduct(*product)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	paths := learnPaths(root)
	ledgerRows, err := readLearnLedger(paths.Ledger)
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn rollback:", err)
		return 2
	}
	target := findLearnLedger(ledgerRows, strings.TrimSpace(*id))
	if target == nil || target.Action != "apply" {
		fmt.Fprintln(os.Stderr, "apply ledger entry not found:", *id)
		return 2
	}
	proposals, err := readLearnProposals(paths.Proposals)
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn rollback:", err)
		return 2
	}
	ruleFile, err := ledgerRuleAbsolutePath(root, target.RulePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn rollback:", err)
		return 2
	}
	current, err := os.ReadFile(ruleFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn rollback:", err)
		return 2
	}
	if learnSHA(current) != target.AfterSHA256 {
		fmt.Fprintln(os.Stderr, "learn rollback: active rule changed since target apply; refusing to clobber newer state")
		return 3
	}
	if target.BeforeExists {
		blobPath := filepath.Join(paths.Root, filepath.FromSlash(target.BeforeBlob))
		before, err := os.ReadFile(blobPath)
		if err != nil || learnSHA(before) != target.BeforeSHA256 {
			fmt.Fprintln(os.Stderr, "learn rollback: before blob missing or digest mismatch")
			return 3
		}
		if err := writeLearnAtomic(ruleFile, before); err != nil {
			fmt.Fprintln(os.Stderr, "learn rollback:", err)
			return 2
		}
	} else if err := os.Remove(ruleFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(os.Stderr, "learn rollback:", err)
		return 2
	}
	if idx, p := findLearnProposal(proposals, target.ProposalID); p != nil {
		proposals[idx].Status = learnRevoked
		pb, err := marshalLearnJSONL(proposals)
		if err != nil || writeLearnAtomic(paths.Proposals, pb) != nil {
			fmt.Fprintln(os.Stderr, "learn rollback: proposal status update failed")
			return 2
		}
	}
	now := time.Now().UTC()
	rid, err := newLearnID("LM", now)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	afterExists := target.BeforeExists
	afterSHA := ""
	if afterExists {
		b, _ := os.ReadFile(ruleFile)
		afterSHA = learnSHA(b)
	}
	row := learnLedgerRecord{
		Schema: learnSchemaVersion, ID: rid, CreatedAt: now.Format(time.RFC3339Nano),
		Action: "rollback", ProposalID: target.ProposalID, Class: target.Class,
		RulePath: target.RulePath, BeforeExists: true, BeforeSHA256: target.AfterSHA256,
		AfterSHA256: afterSHA, GitHead: learnGitHead(root), TargetLedgerID: target.ID,
	}
	if err := appendLearnJSON(paths.Ledger, row); err != nil {
		fmt.Fprintln(os.Stderr, "learn rollback: ledger append failed:", err)
		return 2
	}
	fmt.Printf("%s REVOKED target=%s\n", target.ProposalID, target.ID)
	return 0
}
