package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func seedPatrolRepo(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	initLearnRuleGitRepo(t, root)
}

func writePatrolNode(t *testing.T, root string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(planNodeDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nid: \"N-012_windows\"\ntitle: \"windows\"\nparent: \"p\"\ntrigger: \"t\"\nowner: \"\"\ndone_when: \"d\"\nstatus: \"open\"\nreturn_to: \"p\"\ncreated_at: \"2026-09-28T00:00:00Z\"\nupdated_at: \"2026-09-28T00:00:00Z\"\nreceipts:\n---\n\n## Windows\n" + strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(planNodeDir(root), "N-012_windows.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCuratorPatrolCoverageCompleteFromN012(t *testing.T) {
	product := seedPlanNodeProduct(t)
	repoWorker := filepath.Join(t.TempDir(), "airworker")
	repoStorage := filepath.Join(t.TempDir(), "airstorage")
	seedPatrolRepo(t, repoWorker)
	seedPatrolRepo(t, repoStorage)
	writePatrolNode(t, product,
		"- AC·DEV·AirWorker — https://chatgpt.com/c/worker",
		"- AC·DEV·AirStorage — https://chatgpt.com/c/storage",
	)
	mapping, _ := json.Marshal(map[string][]string{
		"AirWorker":  {repoWorker},
		"AirStorage": {repoStorage},
	})
	t.Setenv("AIR_WORKER_PATROL_REPOS_JSON", string(mapping))

	cov, has, err := buildCuratorPatrolCoverage(product, time.Now().UTC())
	if err != nil || !has {
		t.Fatalf("has=%v err=%v", has, err)
	}
	if !cov.Complete || cov.Expected != 2 || cov.Checked != 2 || cov.Summary != "проверено 2 из 2" || cov.Judge != "PASS" {
		t.Fatalf("coverage=%#v", cov)
	}
	for _, row := range cov.Rows {
		if row.Commit == "" || row.CommitAt == "" || row.State == "" {
			t.Fatalf("incomplete row=%#v", row)
		}
	}
}

func TestPatrolAggregatesDirtyAcrossProductRepos(t *testing.T) {
	repoClean := filepath.Join(t.TempDir(), "clean")
	repoDirty := filepath.Join(t.TempDir(), "dirty")
	seedPatrolRepo(t, repoClean)
	seedPatrolRepo(t, repoDirty)
	if err := os.WriteFile(filepath.Join(repoDirty, "work.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mapping, _ := json.Marshal(map[string][]string{"AirWorker": {repoClean, repoDirty}})
	t.Setenv("AIR_WORKER_PATROL_REPOS_JSON", string(mapping))

	snap, err := newestPatrolRepoSnapshot("AirWorker", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if snap.DirtyRecent < 1 {
		t.Fatalf("recent dirty activity was hidden: %#v", snap)
	}
	if snap.State != "active" && snap.State != "dirty" {
		t.Fatalf("state=%q want active/dirty", snap.State)
	}
}

func TestCuratorTickRejectsIncompletePatrol(t *testing.T) {
	product := seedPlanNodeProduct(t)
	repoWorker := filepath.Join(t.TempDir(), "airworker")
	seedPatrolRepo(t, repoWorker)
	writePatrolNode(t, product,
		"- AC·DEV·AirWorker — https://chatgpt.com/c/worker",
		"- AC·DEV·AirStorage — https://chatgpt.com/c/storage",
	)
	mapping, _ := json.Marshal(map[string][]string{
		"AirWorker":  {repoWorker},
		"AirStorage": {filepath.Join(t.TempDir(), "missing")},
	})
	t.Setenv("AIR_WORKER_PATROL_REPOS_JSON", string(mapping))

	code, out := captureLoopOutput(t, func() int {
		return cmdCuratorTick([]string{"-product", product, "-json"})
	})
	if code != 3 {
		t.Fatalf("code=%d out=%q want reject code 3", code, out)
	}
	var doc struct {
		Patrol curatorPatrolCoverage `json:"patrol"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("json: %v out=%q", err, out)
	}
	if doc.Patrol.Complete || doc.Patrol.Checked != 1 || doc.Patrol.Expected != 2 || doc.Patrol.Summary != "проверено 1 из 2" || doc.Patrol.Judge != "REJECT" {
		t.Fatalf("patrol=%#v", doc.Patrol)
	}
}
