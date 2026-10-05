package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func injectSharedPostEventLogFailure(t *testing.T, product string, s sharedLearningSettings) {
	t.Helper()
	t.Setenv("AW_LEARNING_TEST_BLOCK_LOG_ROOT", s.RuntimeRoot)
	s.Judge = learningAdapterFixture(t, "judge-log-failure")
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(product, sharedLearningConfigFile), b, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestSharedLegacyInspectionPropagatesPermissionAndIOErrors(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "learn", "rules")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, cause := range []error{os.ErrPermission, errors.New("fixture disk I/O error")} {
		for _, path := range []string{filepath.Join(root, ".air-worker", "learn", "RULES.md"), dir} {
			stat := func(p string) (os.FileInfo, error) {
				if p == path {
					return nil, cause
				}
				return os.Lstat(p)
			}
			if err := inspectLegacyLearningRules(root, stat, os.ReadDir); !errors.Is(err, cause) {
				t.Fatalf("lost stat cause: %v", err)
			}
		}
	}
	for _, cause := range []error{os.ErrPermission, os.ErrNotExist, errors.New("fixture directory I/O error")} {
		readDir := func(string) ([]os.DirEntry, error) { return nil, cause }
		if err := inspectLegacyLearningRules(root, os.Lstat, readDir); !errors.Is(err, cause) {
			t.Fatalf("lost ReadDir cause: %v", err)
		}
	}
}

func TestSharedCloseOwnershipConflictDoesNotChangePlan(t *testing.T) {
	product, s, n := prepareSharedPlanClose(t)
	if _, err := executeSharedLearning(product, s, "status", nil); err != nil {
		t.Fatal(err)
	}
	originalNode, err := os.ReadFile(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	originalPlan, err := os.ReadFile(filepath.Join(product, "PLAN.md"))
	if err != nil {
		t.Fatal(err)
	}
	s.RuntimeRoot = filepath.Join(t.TempDir(), "different-owner")
	b, _ := json.Marshal(s)
	if err := os.WriteFile(filepath.Join(product, sharedLearningConfigFile), b, 0600); err != nil {
		t.Fatal(err)
	}
	if code := cmdPlanNodeClose([]string{n.ID, "-product", product, "-receipt", "fixture/pass", "-actor", "worker", "-actor-kind", "gpt-window"}); code == 0 {
		t.Fatal("ownership conflict accepted")
	}
	gotNode, _ := os.ReadFile(n.Path)
	gotPlan, _ := os.ReadFile(filepath.Join(product, "PLAN.md"))
	if string(gotNode) != string(originalNode) || string(gotPlan) != string(originalPlan) {
		t.Fatal("ownership preflight failed after plan mutation")
	}
}

func TestSharedDataFileActorDoesNotNeedAnExplicitFlag(t *testing.T) {
	product, s := sharedProductFixture(t, false)
	payload := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(payload, []byte(`{"run_id":"all-json","observed":"JSON actor survives","actor":"json-worker","actor_kind":"gpt-window","session":"json-session"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if on, code := routeSharedLearn([]string{"finalize", "-product", product, "-data-file", payload}); !on || code != 0 {
		t.Fatalf("%v %d", on, code)
	}
	rows := sharedEventRows(t, s)
	if rows[0]["principal"] != "gpt-window:json-worker" || rows[0]["run_id"] != "all-json" {
		t.Fatal(rows)
	}
}

func TestSharedCloseInitialIOFailureDoesNotChangeNode(t *testing.T) {
	product, s, n := prepareSharedPlanClose(t)
	before, err := os.ReadFile(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.RuntimeRoot, "logs"), []byte("fixture preflight obstruction"), 0600); err != nil {
		t.Fatal(err)
	}
	if code := cmdPlanNodeClose([]string{n.ID, "-product", product, "-receipt", "fixture/pass", "-actor", "worker", "-actor-kind", "gpt-window"}); code == 0 {
		t.Fatal("initial I/O error accepted")
	}
	after, _ := os.ReadFile(n.Path)
	if string(before) != string(after) {
		t.Fatal("node changed before module preflight succeeded")
	}
	rows, err := filepath.Glob(filepath.Join(s.RuntimeRoot, "plan-close", "*.json"))
	if err != nil || len(rows) != 0 {
		t.Fatalf("premature intent: %v %v", rows, err)
	}
}
