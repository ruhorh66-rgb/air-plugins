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

	"github.com/ruhorh66-rgb/air-modules/learning"
)

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
	var rules []learnRuleRecord
	var paths []string
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
		if _, err := os.Stat(target); err == nil {
			return nil, nil, fmt.Errorf("managed procedure target already exists: %s", target)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, nil, err
		}
		rules = append(rules, rule)
		paths = append(paths, path)
	}
	sort.SliceStable(rules, func(i, j int) bool { return rules[i].ProposalID < rules[j].ProposalID })
	sort.Strings(paths)
	return rules, paths, nil
}

func archiveLegacyRules(product string, sourcePaths []string) (string, []string, error) {
	if len(sourcePaths) == 0 {
		return "", nil, nil
	}
	archive := filepath.Join(product, "learn", "legacy-rules")
	if err := os.MkdirAll(archive, 0o755); err != nil {
		return "", nil, err
	}
	moved := make([]string, 0, len(sourcePaths))
	for _, src := range sourcePaths {
		dst := filepath.Join(archive, filepath.Base(src))
		if existing, err := os.ReadFile(dst); err == nil {
			incoming, rerr := os.ReadFile(src)
			if rerr != nil || !bytes.Equal(existing, incoming) {
				return archive, moved, fmt.Errorf("legacy archive collision: %s", dst)
			}
			if err := os.Remove(src); err != nil {
				return archive, moved, err
			}
			moved = append(moved, src)
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return archive, moved, err
		}
		if err := os.Rename(src, dst); err != nil {
			return archive, moved, err
		}
		moved = append(moved, src)
	}
	_ = os.Remove(filepath.Join(product, "learn", "rules"))
	return archive, moved, nil
}

func restoreLegacyRules(product, archive string, sourcePaths []string) {
	if archive == "" {
		return
	}
	_ = os.MkdirAll(filepath.Join(product, "learn", "rules"), 0o755)
	for _, src := range sourcePaths {
		dst := filepath.Join(archive, filepath.Base(src))
		if _, err := os.Stat(dst); err == nil {
			_ = os.Rename(dst, src)
		}
	}
	_ = os.Remove(archive)
}

func writeSharedLearningSettings(product string, s sharedLearningSettings) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(product, sharedLearningConfigFile), append(b, '\n'))
}

func initSharedLearning(product, runtimeRoot, productID string) (sharedLearningInitReport, error) {
	var report sharedLearningInitReport
	root, err := normalizeLearnProduct(product)
	if err != nil {
		return report, err
	}
	runtimeRoot = filepath.Clean(strings.TrimSpace(runtimeRoot))
	if !filepath.IsAbs(runtimeRoot) {
		return report, errors.New("-runtime-root must be an absolute path")
	}
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
		if _, execErr := executeSharedLearning(root, s, "status", nil); execErr != nil {
			return report, execErr
		}
		return sharedLearningInitReport{
			Schema: "air-worker.shared-learning-init/v1", Status: "already_initialized",
			Product: root, ProductID: s.ProductID, RuntimeRoot: s.RuntimeRoot, ConfigPath: configPath,
			Reviewer: s.Reviewer.Executable, SharedVersion: learning.Version,
		}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return report, err
	}

	rules, sourcePaths, err := loadLegacyOperationalProcedures(root)
	if err != nil {
		return report, err
	}
	runtimeExisted := false
	if st, err := os.Stat(runtimeRoot); err == nil {
		runtimeExisted = true
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

	s := sharedLearningSettings{
		Schema: "air-worker.shared-learning/v1", ProductID: productID, RuntimeRoot: runtimeRoot,
		ManagedSkillPrefix: "skills/learned", TimeoutMS: 300000,
		ProtectedTargets: []string{"PLAN.md", "cmd", "bin", "hooks", "skills/air-curator", "skills/woody"},
		Reviewer: &learningProcessAdapter{
			Executable: "@self", SHA256: "@self", TimeoutMS: 240000,
			Args: []string{"learning-adapter", "reviewer", "-model", "gpt-5.6-luna", "-effort", "medium"},
		},
	}

	archive, moved, err := archiveLegacyRules(root, sourcePaths)
	if err != nil {
		restoreLegacyRules(root, archive, moved)
		return report, err
	}
	configWritten := false
	managedCreated := []string{}
	rollback := func() {
		if configWritten {
			_ = os.Remove(configPath)
		}
		for _, target := range managedCreated {
			_ = os.Remove(target)
		}
		if !runtimeExisted {
			_ = os.RemoveAll(runtimeRoot)
		}
		restoreLegacyRules(root, archive, moved)
	}
	if err := writeSharedLearningSettings(root, s); err != nil {
		rollback()
		return report, err
	}
	configWritten = true
	checked, on, err := readSharedLearningSettings(root)
	if err != nil || !on {
		rollback()
		if err == nil {
			err = errors.New("shared learning did not activate")
		}
		return report, err
	}
	for _, rule := range rules {
		target := learningProcedureTarget(checked.ManagedSkillPrefix, rule.Class)
		res, err := executeSharedLearning(root, checked, "propose", map[string]string{
			"kind": "procedure", "target": target, "pre_sha256": "", "content": legacyRuleProcedure(rule),
		})
		if err != nil || res.Status != "applied" {
			rollback()
			if err == nil {
				err = fmt.Errorf("legacy procedure %s migration status %s", rule.ProposalID, res.Status)
			}
			return report, err
		}
		managedCreated = append(managedCreated, filepath.Join(root, filepath.FromSlash(target)))
		report.MigratedRules = append(report.MigratedRules, rule.ProposalID)
		report.ManagedSkills = append(report.ManagedSkills, target)
	}
	if _, err := executeSharedLearning(root, checked, "status", nil); err != nil {
		rollback()
		return report, err
	}
	report.Schema = "air-worker.shared-learning-init/v1"
	report.Status = "initialized"
	report.Product = root
	report.ProductID = productID
	report.RuntimeRoot = runtimeRoot
	report.ConfigPath = configPath
	report.Reviewer = "@self"
	report.LegacyArchive = archive
	report.SharedVersion = learning.Version
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
