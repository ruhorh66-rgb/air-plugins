package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/ruhorh66-rgb/air-modules/learning"
)

const sharedLearningBootstrapSchema = "air-worker.shared-learning-bootstrap/v1"

type sharedLearningInitReport struct {
	Schema        string   `json:"schema"`
	Status        string   `json:"status"`
	Product       string   `json:"product"`
	ProductID     string   `json:"product_id"`
	RuntimeRoot   string   `json:"runtime_root"`
	ConfigPath    string   `json:"config_path"`
	Reviewer      string   `json:"reviewer"`
	MigratedRules []string `json:"migrated_rules,omitempty"`
	ManagedSkills []string `json:"managed_skills,omitempty"`
	LegacyArchive string   `json:"legacy_archive,omitempty"`
	SharedVersion string   `json:"shared_version"`
}

type sharedLearningBootstrapRule struct {
	LegacyProposalID    string `json:"legacy_proposal_id"`
	SourcePath          string `json:"source_path"`
	ArchivePath         string `json:"archive_path"`
	LegacySHA256        string `json:"legacy_sha256"`
	MigrationProposalID string `json:"migration_proposal_id"`
	Target              string `json:"target"`
	ProcedureSHA256     string `json:"procedure_sha256"`
}

type sharedLearningBootstrapIntent struct {
	Schema             string                        `json:"schema"`
	ProductID          string                        `json:"product_id"`
	RuntimeRoot        string                        `json:"runtime_root"`
	ManagedSkillPrefix string                        `json:"managed_skill_prefix"`
	Status             string                        `json:"status"`
	CreatedAt          string                        `json:"created_at"`
	UpdatedAt          string                        `json:"updated_at"`
	CompletedAt        string                        `json:"completed_at,omitempty"`
	Rules              []sharedLearningBootstrapRule `json:"rules"`
}

type sharedLearningOwnerBinding struct {
	Schema      string `json:"schema"`
	ProductID   string `json:"product_id"`
	RuntimeRoot string `json:"runtime_root"`
}

var sharedLearningBootstrapExecute = executeSharedLearning
var sharedLearningBootstrapFault = func(string) error { return nil }

func sameLearningPath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func legacyRuleProcedure(rule learnRuleRecord) string {
	return fmt.Sprintf("# %s\n\n## When to apply\n%s\n\n## Procedure\n1. %s\n\n## Pitfalls\n- Verification: %s\n- Legacy proposal: %s\n- Legacy approval: %s\n- Legacy verified at: %s\n- Legacy verification receipt: %s\n",
		strings.TrimSpace(rule.Class),
		strings.TrimSpace(rule.Trigger),
		strings.TrimSpace(rule.Rule),
		strings.TrimSpace(rule.TestCase),
		strings.TrimSpace(rule.ProposalID),
		strings.TrimSpace(rule.Approval),
		strings.TrimSpace(rule.VerifiedAt),
		strings.TrimSpace(rule.VerificationReceipt),
	)
}

func loadLegacyOperationalProcedures(product string) ([]learnRuleRecord, []string, error) {
	dir := filepath.Join(product, "learn", "rules")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	type pair struct {
		rule learnRuleRecord
		path string
	}
	var pairs []pair
	for _, entry := range entries {
		if entry.IsDir() {
			return nil, nil, fmt.Errorf("legacy rule directory contains subdirectory %s", entry.Name())
		}
		path := filepath.Join(dir, entry.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		var rule learnRuleRecord
		if err := json.Unmarshal(b, &rule); err != nil {
			return nil, nil, fmt.Errorf("legacy rule %s: %w", entry.Name(), err)
		}
		if err := validateLearnRuleRecord(rule); err != nil {
			return nil, nil, fmt.Errorf("legacy rule %s: %w", entry.Name(), err)
		}
		if rule.Status != learnApplied || !strings.EqualFold(strings.TrimSpace(rule.CheckSpec), "operational-procedure-v1") {
			return nil, nil, fmt.Errorf("legacy rule %s is not an applied operational-procedure-v1 and requires explicit reconciliation", rule.ProposalID)
		}
		target := filepath.Join(product, filepath.FromSlash(learningProcedureTarget("skills/learned", rule.Class)))
		if existing, err := os.ReadFile(target); err == nil {
			expected := []byte(legacyRuleProcedure(rule))
			if learnSHA(existing) != learnSHA(expected) || !bytes.Equal(existing, expected) {
				return nil, nil, fmt.Errorf("managed procedure target already exists with different bytes: %s", target)
			}
			// Exact packaged procedure bytes are a reproducibility seed only. A new
			// runtime still has to reconstruct ownership/provenance through the
			// shared module before those bytes can count as managed state.
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, nil, err
		}
		pairs = append(pairs, pair{rule: rule, path: path})
	}
	sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].rule.ProposalID < pairs[j].rule.ProposalID })
	rules := make([]learnRuleRecord, 0, len(pairs))
	paths := make([]string, 0, len(pairs))
	for _, item := range pairs {
		rules = append(rules, item.rule)
		paths = append(paths, item.path)
	}
	return rules, paths, nil
}

func sharedLearningBootstrapIntentPath(runtimeRoot string) string {
	return filepath.Join(runtimeRoot, "bootstrap.json")
}

func cleanBootstrapRelative(rel, prefix string) (string, error) {
	rel = filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.TrimSpace(rel))))
	if rel == "." || filepath.IsAbs(filepath.FromSlash(rel)) || !filepath.IsLocal(filepath.FromSlash(rel)) ||
		strings.HasPrefix(rel, "../") || rel == ".." || !strings.HasPrefix(rel, prefix) {
		return "", errors.New("invalid shared-learning bootstrap relative path")
	}
	return rel, nil
}

func validateSharedLearningBootstrapIntent(intent sharedLearningBootstrapIntent, runtimeRoot string) error {
	if intent.Schema != sharedLearningBootstrapSchema || strings.TrimSpace(intent.ProductID) == "" ||
		!sameLearningPath(intent.RuntimeRoot, runtimeRoot) || intent.ManagedSkillPrefix != "skills/learned" {
		return errors.New("invalid shared-learning bootstrap identity")
	}
	switch intent.Status {
	case "prepared", "ready", "complete":
	default:
		return errors.New("invalid shared-learning bootstrap status")
	}
	seenProposal := map[string]bool{}
	seenTarget := map[string]bool{}
	for _, rule := range intent.Rules {
		if strings.TrimSpace(rule.LegacyProposalID) == "" || strings.TrimSpace(rule.MigrationProposalID) == "" ||
			len(rule.LegacySHA256) != 64 || len(rule.ProcedureSHA256) != 64 {
			return errors.New("invalid shared-learning bootstrap rule identity")
		}
		if seenProposal[rule.MigrationProposalID] || seenTarget[strings.ToLower(rule.Target)] {
			return errors.New("duplicate shared-learning bootstrap rule identity")
		}
		seenProposal[rule.MigrationProposalID] = true
		seenTarget[strings.ToLower(rule.Target)] = true
		if _, err := cleanBootstrapRelative(rule.SourcePath, "learn/rules/"); err != nil {
			return err
		}
		if _, err := cleanBootstrapRelative(rule.ArchivePath, "learn/legacy-rules/"); err != nil {
			return err
		}
		if _, err := cleanBootstrapRelative(rule.Target, intent.ManagedSkillPrefix+"/"); err != nil || !strings.HasSuffix(rule.Target, ".md") {
			return errors.New("invalid shared-learning bootstrap target")
		}
	}
	return nil
}

func readSharedLearningBootstrapIntent(runtimeRoot string) (sharedLearningBootstrapIntent, bool, error) {
	var intent sharedLearningBootstrapIntent
	path := sharedLearningBootstrapIntentPath(runtimeRoot)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return intent, false, nil
	}
	if err != nil {
		return intent, true, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return intent, true, errors.New("shared-learning bootstrap intent is not a regular file")
	}
	b, err := readLearningBounded(path, sharedLearningMaxBytes)
	if err != nil {
		return intent, true, err
	}
	if err := json.Unmarshal(b, &intent); err != nil {
		return intent, true, err
	}
	if err := validateSharedLearningBootstrapIntent(intent, runtimeRoot); err != nil {
		return intent, true, err
	}
	return intent, true, nil
}

func writeSharedLearningBootstrapIntent(runtimeRoot string, intent sharedLearningBootstrapIntent) error {
	if err := os.MkdirAll(runtimeRoot, 0o700); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if intent.CreatedAt == "" {
		intent.CreatedAt = now
	}
	intent.UpdatedAt = now
	if intent.Status == "complete" && intent.CompletedAt == "" {
		intent.CompletedAt = now
	}
	if err := validateSharedLearningBootstrapIntent(intent, runtimeRoot); err != nil {
		return err
	}
	b, err := json.MarshalIndent(intent, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomicDurable(sharedLearningBootstrapIntentPath(runtimeRoot), append(b, '\n'))
}

func buildSharedLearningBootstrapIntent(product, runtimeRoot, productID string, rules []learnRuleRecord, sourcePaths []string) (sharedLearningBootstrapIntent, error) {
	if len(rules) != len(sourcePaths) {
		return sharedLearningBootstrapIntent{}, errors.New("legacy bootstrap rule/path cardinality mismatch")
	}
	intent := sharedLearningBootstrapIntent{
		Schema: sharedLearningBootstrapSchema, ProductID: productID, RuntimeRoot: runtimeRoot,
		ManagedSkillPrefix: "skills/learned", Status: "prepared", Rules: make([]sharedLearningBootstrapRule, 0, len(rules)),
	}
	seenTarget := map[string]bool{}
	for i, rule := range rules {
		raw, err := os.ReadFile(sourcePaths[i])
		if err != nil {
			return sharedLearningBootstrapIntent{}, err
		}
		var check learnRuleRecord
		if json.Unmarshal(raw, &check) != nil || check.ProposalID != rule.ProposalID {
			return sharedLearningBootstrapIntent{}, errors.New("legacy rule changed while bootstrap intent was prepared")
		}
		rel, err := filepath.Rel(product, sourcePaths[i])
		if err != nil {
			return sharedLearningBootstrapIntent{}, err
		}
		sourceRel := filepath.ToSlash(rel)
		archiveRel := filepath.ToSlash(filepath.Join("learn", "legacy-rules", filepath.Base(sourcePaths[i])))
		target := learningProcedureTarget(intent.ManagedSkillPrefix, rule.Class)
		if seenTarget[strings.ToLower(target)] {
			return sharedLearningBootstrapIntent{}, fmt.Errorf("multiple legacy rules map to one managed procedure target: %s", target)
		}
		seenTarget[strings.ToLower(target)] = true
		procedure := legacyRuleProcedure(rule)
		intent.Rules = append(intent.Rules, sharedLearningBootstrapRule{
			LegacyProposalID: rule.ProposalID, SourcePath: sourceRel, ArchivePath: archiveRel,
			LegacySHA256: learnSHA(raw), MigrationProposalID: "LP-MIG-" + learnSHA([]byte(rule.ProposalID))[:24],
			Target: target, ProcedureSHA256: learnSHA([]byte(procedure)),
		})
	}
	return intent, nil
}

func sharedLearningSettingsForBootstrap(productID, runtimeRoot string) sharedLearningSettings {
	return sharedLearningSettings{
		Schema: "air-worker.shared-learning/v1", ProductID: productID, RuntimeRoot: runtimeRoot,
		ManagedSkillPrefix: "skills/learned", TimeoutMS: 300000,
		ProtectedTargets: []string{"PLAN.md", "cmd", "bin", "hooks", "skills/air-curator", "skills/woody"},
		Reviewer: &learningProcessAdapter{
			Executable: "@self", SHA256: "@self", TimeoutMS: 240000,
			Args: []string{"learning-adapter", "reviewer", "-model", "gpt-5.6-luna", "-effort", "medium"},
		},
	}
}

func writeSharedLearningSettings(product string, s sharedLearningSettings) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(product, sharedLearningConfigFile), append(b, '\n'))
}

func writeSharedLearningSettingsDurable(product string, s sharedLearningSettings) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomicDurable(filepath.Join(product, sharedLearningConfigFile), append(b, '\n'))
}

func sharedBootstrapLegacyBytes(product string, rule sharedLearningBootstrapRule) ([]byte, error) {
	var found []byte
	for _, rel := range []string{rule.SourcePath, rule.ArchivePath} {
		path := filepath.Join(product, filepath.FromSlash(rel))
		b, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if learnSHA(b) != rule.LegacySHA256 {
			return nil, fmt.Errorf("legacy bootstrap provenance SHA mismatch: %s", rel)
		}
		if found != nil && !bytes.Equal(found, b) {
			return nil, fmt.Errorf("legacy bootstrap source/archive disagree: %s", rule.LegacyProposalID)
		}
		found = b
	}
	if found == nil {
		return nil, fmt.Errorf("legacy bootstrap provenance missing: %s", rule.LegacyProposalID)
	}
	return found, nil
}

func sharedBootstrapLegacyRule(product string, rule sharedLearningBootstrapRule) (learnRuleRecord, string, error) {
	b, err := sharedBootstrapLegacyBytes(product, rule)
	if err != nil {
		return learnRuleRecord{}, "", err
	}
	var legacy learnRuleRecord
	if err := json.Unmarshal(b, &legacy); err != nil {
		return learnRuleRecord{}, "", err
	}
	if err := validateLearnRuleRecord(legacy); err != nil {
		return learnRuleRecord{}, "", err
	}
	if legacy.ProposalID != rule.LegacyProposalID || legacy.Status != learnApplied ||
		!strings.EqualFold(strings.TrimSpace(legacy.CheckSpec), "operational-procedure-v1") {
		return learnRuleRecord{}, "", errors.New("legacy bootstrap rule no longer matches approved operational procedure")
	}
	procedure := legacyRuleProcedure(legacy)
	if learnSHA([]byte(procedure)) != rule.ProcedureSHA256 {
		return learnRuleRecord{}, "", errors.New("legacy bootstrap procedure SHA mismatch")
	}
	return legacy, procedure, nil
}

func verifySharedBootstrapMigration(product string, s sharedLearningSettings, rule sharedLearningBootstrapRule) error {
	targetPath := filepath.Join(product, filepath.FromSlash(rule.Target))
	body, err := readLearningBounded(targetPath, 64*1024)
	if err != nil {
		return err
	}
	if learnSHA(body) != rule.ProcedureSHA256 {
		return fmt.Errorf("managed procedure SHA mismatch: %s", rule.Target)
	}
	var last map[string]any
	err = scanLearnJSONL(filepath.Join(s.RuntimeRoot, "ledger.jsonl"), func(raw []byte) error {
		var row map[string]any
		if err := json.Unmarshal(raw, &row); err != nil {
			return err
		}
		if value, _ := row["proposal_id"].(string); value == rule.MigrationProposalID {
			last = row
		}
		return nil
	})
	if err != nil {
		return err
	}
	if last == nil {
		return fmt.Errorf("shared migration ledger missing: %s", rule.MigrationProposalID)
	}
	text := func(key string) string {
		value, _ := last[key].(string)
		return value
	}
	if text("kind") != "procedure" || text("status") != "applied" || text("target") != rule.Target ||
		text("post_sha256") != rule.ProcedureSHA256 {
		return fmt.Errorf("shared migration ledger mismatch: %s", rule.MigrationProposalID)
	}
	return nil
}

func verifySharedBootstrapState(product string, s sharedLearningSettings, intent sharedLearningBootstrapIntent) error {
	if err := validateSharedLearningBootstrapIntent(intent, s.RuntimeRoot); err != nil {
		return err
	}
	if intent.ProductID != s.ProductID || intent.ManagedSkillPrefix != s.ManagedSkillPrefix {
		return errors.New("shared-learning bootstrap/settings identity mismatch")
	}
	for _, rule := range intent.Rules {
		if _, _, err := sharedBootstrapLegacyRule(product, rule); err != nil {
			return err
		}
		if err := verifySharedBootstrapMigration(product, s, rule); err != nil {
			return err
		}
	}
	return nil
}

func ensureLegacyRulesMDBootstrapSafe(product string) error {
	path := filepath.Join(product, ".air-worker", "learn", "RULES.md")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 0 {
		return errors.New("active legacy RULES.md requires explicit reconciliation before shared bootstrap")
	}
	return nil
}

func verifySharedBootstrapLegacyOverlap(product string, intent sharedLearningBootstrapIntent) error {
	if err := ensureLegacyRulesMDBootstrapSafe(product); err != nil {
		return err
	}
	dir := filepath.Join(product, "learn", "rules")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	allowed := map[string]sharedLearningBootstrapRule{}
	for _, rule := range intent.Rules {
		allowed[filepath.ToSlash(rule.SourcePath)] = rule
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return errors.New("legacy rule overlap contains a directory")
		}
		rel := filepath.ToSlash(filepath.Join("learn", "rules", entry.Name()))
		rule, ok := allowed[rel]
		if !ok {
			return fmt.Errorf("legacy rule is not covered by shared bootstrap intent: %s", rel)
		}
		b, err := os.ReadFile(filepath.Join(product, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		if learnSHA(b) != rule.LegacySHA256 {
			return fmt.Errorf("legacy rule changed after shared bootstrap readiness: %s", rel)
		}
	}
	return nil
}

// allowSharedLearningBootstrapLegacy permits only the narrow crash-recovery window
// after the selector is durably published and before every exact legacy source was
// moved to its provenance archive. Any extra/mutated rule still fails closed.
func allowSharedLearningBootstrapLegacy(product string, s sharedLearningSettings) error {
	intent, found, err := readSharedLearningBootstrapIntent(s.RuntimeRoot)
	if err != nil || !found {
		if err == nil {
			err = errors.New("shared bootstrap intent missing")
		}
		return err
	}
	if intent.Status != "ready" && intent.Status != "complete" {
		return errors.New("shared bootstrap intent is not ready")
	}
	if err := verifySharedBootstrapState(product, s, intent); err != nil {
		return err
	}
	return verifySharedBootstrapLegacyOverlap(product, intent)
}

func archiveSharedBootstrapLegacyRules(product string, intent sharedLearningBootstrapIntent) (string, error) {
	if len(intent.Rules) == 0 {
		return "", nil
	}
	archiveDir := filepath.Join(product, "learn", "legacy-rules")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return "", err
	}
	for _, rule := range intent.Rules {
		src := filepath.Join(product, filepath.FromSlash(rule.SourcePath))
		dst := filepath.Join(product, filepath.FromSlash(rule.ArchivePath))
		source, srcErr := os.ReadFile(src)
		if srcErr == nil {
			if learnSHA(source) != rule.LegacySHA256 {
				return archiveDir, fmt.Errorf("legacy rule changed before archive: %s", rule.SourcePath)
			}
			if existing, err := os.ReadFile(dst); err == nil {
				if learnSHA(existing) != rule.LegacySHA256 || !bytes.Equal(existing, source) {
					return archiveDir, fmt.Errorf("legacy archive collision: %s", dst)
				}
			} else if errors.Is(err, os.ErrNotExist) {
				if err := writeFileAtomicDurable(dst, source); err != nil {
					return archiveDir, err
				}
			} else {
				return archiveDir, err
			}
			// The source rule is a Git-tracked reproducibility seed, not the active
			// shared-learning writer. Keep it byte-identical so a fresh clone can
			// reconstruct a new machine-local runtime; shared mode keeps this legacy
			// state read-only and verifies the exact bootstrap overlap.
			continue
		}
		if !errors.Is(srcErr, os.ErrNotExist) {
			return archiveDir, srcErr
		}
		existing, err := os.ReadFile(dst)
		if err != nil || learnSHA(existing) != rule.LegacySHA256 {
			if err == nil {
				err = errors.New("legacy archive SHA mismatch")
			}
			return archiveDir, err
		}
	}
	return archiveDir, nil
}

func verifyBootstrapOwnerBinding(product, runtimeRoot, productID string, intentFound bool) error {
	path := filepath.Join(product, ".air-learning-owner.json")
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var owner sharedLearningOwnerBinding
	if json.Unmarshal(b, &owner) != nil || owner.Schema != "air.learning.owner/v1" ||
		owner.ProductID != productID || !sameLearningPath(owner.RuntimeRoot, runtimeRoot) {
		return errors.New("existing shared-learning owner conflicts with requested bootstrap identity")
	}
	if !intentFound {
		return errors.New("shared-learning owner exists without bootstrap intent; explicit reconciliation required")
	}
	return nil
}

func ensureNoUntrackedLegacyArchive(product string) error {
	dir := filepath.Join(product, "learn", "legacy-rules")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return errors.New("legacy-rules archive exists without bootstrap intent; explicit reconciliation required")
	}
	return nil
}

func sharedLearningBootstrapReport(product string, s sharedLearningSettings, intent sharedLearningBootstrapIntent, status string) sharedLearningInitReport {
	report := sharedLearningInitReport{
		Schema: "air-worker.shared-learning-init/v1", Status: status, Product: product,
		ProductID: s.ProductID, RuntimeRoot: s.RuntimeRoot, ConfigPath: filepath.Join(product, sharedLearningConfigFile),
		Reviewer: "@self", SharedVersion: learning.Version,
	}
	if len(intent.Rules) > 0 {
		report.LegacyArchive = filepath.Join(product, "learn", "legacy-rules")
	}
	for _, rule := range intent.Rules {
		report.MigratedRules = append(report.MigratedRules, rule.LegacyProposalID)
		report.ManagedSkills = append(report.ManagedSkills, rule.Target)
	}
	return report
}

func initSharedLearning(product, runtimeRoot, productID string) (sharedLearningInitReport, error) {
	var report sharedLearningInitReport
	root, err := normalizeLearnProduct(product)
	if err != nil {
		return report, err
	}
	if strings.TrimSpace(runtimeRoot) == "" {
		return report, errors.New("-runtime-root is required")
	}
	if err := validateSharedLearningBootstrapRuntimeInput(runtimeRoot); err != nil {
		return report, err
	}
	if !filepath.IsAbs(runtimeRoot) {
		return report, errors.New("-runtime-root must be an absolute path")
	}
	runtimeRoot, err = canonicalSharedLearningBootstrapRuntime(runtimeRoot)
	if err != nil {
		return report, err
	}
	unlock, err := acquireSharedLearningBootstrapLock(root, runtimeRoot, 30*time.Second)
	if err != nil {
		return report, err
	}
	defer unlock()
	productID = strings.TrimSpace(productID)
	if productID == "" {
		productID = filepath.Base(root)
	}
	configPath := filepath.Join(root, sharedLearningConfigFile)

	if _, err := os.Lstat(configPath); err == nil {
		s, on, readErr := readSharedLearningSettings(root)
		if readErr != nil {
			return report, readErr
		}
		if !on || !sameLearningPath(s.RuntimeRoot, runtimeRoot) || s.ProductID != productID {
			return report, errors.New("learning-module.json already exists with a different identity")
		}
		intent, found, intentErr := readSharedLearningBootstrapIntent(s.RuntimeRoot)
		if intentErr != nil || !found {
			if intentErr == nil {
				intentErr = errors.New("learning-module.json exists without bootstrap intent")
			}
			return report, intentErr
		}
		if intent.Status != "ready" && intent.Status != "complete" {
			return report, errors.New("learning-module.json exists before bootstrap readiness")
		}
		if _, err := sharedLearningBootstrapExecute(root, s, "status", nil); err != nil {
			return report, err
		}
		if err := verifySharedBootstrapState(root, s, intent); err != nil {
			return report, err
		}
		wasComplete := intent.Status == "complete"
		if _, err := archiveSharedBootstrapLegacyRules(root, intent); err != nil {
			return report, err
		}
		intent.Status = "complete"
		if err := writeSharedLearningBootstrapIntent(s.RuntimeRoot, intent); err != nil {
			return report, err
		}
		status := "recovered"
		if wasComplete {
			status = "already_initialized"
		}
		return sharedLearningBootstrapReport(root, s, intent, status), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return report, err
	}

	intent, intentFound, err := readSharedLearningBootstrapIntent(runtimeRoot)
	if err != nil {
		return report, err
	}
	if err := verifyBootstrapOwnerBinding(root, runtimeRoot, productID, intentFound); err != nil {
		return report, err
	}
	if !intentFound {
		if err := ensureNoUntrackedLegacyArchive(root); err != nil {
			return report, err
		}
		if err := ensureLegacyRulesMDBootstrapSafe(root); err != nil {
			return report, err
		}
		if st, err := os.Stat(runtimeRoot); err == nil {
			if !st.IsDir() {
				return report, errors.New("runtime root exists and is not a directory")
			}
			entries, err := os.ReadDir(runtimeRoot)
			if err != nil {
				return report, err
			}
			if len(entries) != 0 {
				return report, errors.New("runtime root must be empty for first shared-learning initialization")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return report, err
		}
		rules, sourcePaths, err := loadLegacyOperationalProcedures(root)
		if err != nil {
			return report, err
		}
		intent, err = buildSharedLearningBootstrapIntent(root, runtimeRoot, productID, rules, sourcePaths)
		if err != nil {
			return report, err
		}
		if err := writeSharedLearningBootstrapIntent(runtimeRoot, intent); err != nil {
			return report, err
		}
		intentFound = true
		if err := sharedLearningBootstrapFault("after-intent"); err != nil {
			return report, err
		}
	}
	if err := validateSharedLearningBootstrapIntent(intent, runtimeRoot); err != nil {
		return report, err
	}
	if intent.ProductID != productID {
		return report, errors.New("bootstrap intent belongs to a different product id")
	}
	if intent.Status == "complete" {
		return report, errors.New("complete shared-learning bootstrap intent exists without selector; explicit reconciliation required")
	}

	s := sharedLearningSettingsForBootstrap(productID, runtimeRoot)
	if _, err := sharedLearningBootstrapExecute(root, s, "status", nil); err != nil {
		return report, err
	}

	for _, rule := range intent.Rules {
		if err := verifySharedBootstrapMigration(root, s, rule); err == nil {
			continue
		}
		_, procedure, err := sharedBootstrapLegacyRule(root, rule)
		if err != nil {
			return report, err
		}
		preSHA := ""
		targetPath := filepath.Join(root, filepath.FromSlash(rule.Target))
		if existing, readErr := os.ReadFile(targetPath); readErr == nil {
			if learnSHA(existing) != rule.ProcedureSHA256 || !bytes.Equal(existing, []byte(procedure)) {
				return report, fmt.Errorf("packaged managed procedure differs from verified legacy seed: %s", rule.Target)
			}
			// A release may already contain the exact managed procedure bytes while
			// this machine has no runtime ledger yet. Re-propose the identical bytes
			// against their current SHA so the new runtime reconstructs provenance
			// through the module instead of treating packaged content as proof.
			preSHA = rule.ProcedureSHA256
		} else if !errors.Is(readErr, os.ErrNotExist) {
			return report, readErr
		}
		res, execErr := sharedLearningBootstrapExecute(root, s, "propose", map[string]string{
			"proposal_id": rule.MigrationProposalID,
			"kind":        "procedure", "target": rule.Target, "pre_sha256": preSHA, "content": procedure,
		})
		if execErr != nil {
			// The shared module may have changed the target and left a WAL before
			// reporting the error. A status call enters the module under its own
			// locks and performs recovery; never delete runtime/WAL evidence here.
			if _, recoveryErr := sharedLearningBootstrapExecute(root, s, "status", nil); recoveryErr != nil {
				return report, fmt.Errorf("shared migration %s failed (%v) and recovery failed: %w", rule.LegacyProposalID, execErr, recoveryErr)
			}
			if verifyErr := verifySharedBootstrapMigration(root, s, rule); verifyErr == nil {
				continue
			}
			return report, fmt.Errorf("shared migration %s needs retry after recovery: %w", rule.LegacyProposalID, execErr)
		}
		if res.Status != "applied" {
			return report, fmt.Errorf("legacy procedure %s migration status %s", rule.LegacyProposalID, res.Status)
		}
		if err := verifySharedBootstrapMigration(root, s, rule); err != nil {
			return report, err
		}
	}

	intent.Status = "ready"
	if err := writeSharedLearningBootstrapIntent(runtimeRoot, intent); err != nil {
		return report, err
	}
	if err := sharedLearningBootstrapFault("after-ready"); err != nil {
		return report, err
	}
	if err := writeSharedLearningSettingsDurable(root, s); err != nil {
		return report, err
	}
	if checked, on, err := readSharedLearningSettings(root); err != nil || !on {
		if err == nil {
			err = errors.New("shared learning did not activate")
		}
		return report, err
	} else if !sameLearningPath(checked.RuntimeRoot, runtimeRoot) || checked.ProductID != productID {
		return report, errors.New("published shared-learning selector identity changed")
	}
	if err := sharedLearningBootstrapFault("after-selector"); err != nil {
		return report, err
	}
	archive, err := archiveSharedBootstrapLegacyRules(root, intent)
	if err != nil {
		return report, err
	}
	if err := sharedLearningBootstrapFault("after-archive"); err != nil {
		return report, err
	}
	intent.Status = "complete"
	if err := writeSharedLearningBootstrapIntent(runtimeRoot, intent); err != nil {
		return report, err
	}
	if _, err := sharedLearningBootstrapExecute(root, s, "status", nil); err != nil {
		return report, err
	}
	report = sharedLearningBootstrapReport(root, s, intent, "initialized")
	report.LegacyArchive = archive
	return report, nil
}

func cmdLearnInitShared(argv []string) int {
	fs := flag.NewFlagSet("learn init-shared", flag.ContinueOnError)
	product := fs.String("product", "", "корень продукта")
	runtimeRoot := fs.String("runtime-root", "", "абсолютный runtime root общего learning")
	productID := fs.String("product-id", "", "стабильный product id")
	asJSON := fs.Bool("json", true, "JSON output")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if strings.TrimSpace(*runtimeRoot) == "" {
		fmt.Fprintln(os.Stderr, "-runtime-root is required")
		return 2
	}
	report, err := initSharedLearning(*product, *runtimeRoot, *productID)
	if err != nil {
		if *asJSON {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"schema": "air-worker.shared-learning-init/v1", "status": "error", "error": err.Error()})
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		return 2
	}
	if *asJSON {
		if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
			return 2
		}
	} else {
		fmt.Printf("%s %s\n", report.Status, report.RuntimeRoot)
	}
	return 0
}
