package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func captureSelfLoad(t *testing.T, args []string) (int, []byte) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	before := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = before; r.Close() }()
	done := make(chan []byte, 1)
	go func() { body, _ := io.ReadAll(r); done <- body }()
	code := cmdLearn(args)
	w.Close()
	return code, <-done
}

func selfLoadCatalog(t *testing.T, product string, settings sharedLearningSettings) []sharedLearningCatalogSkill {
	t.Helper()
	index, err := executeSharedLearning(product, settings, "index", nil)
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Skills []sharedLearningCatalogSkill `json:"skills"`
	}
	if err := json.Unmarshal(index.Data, &catalog); err != nil {
		t.Fatal(err)
	}
	return catalog.Skills
}

func selfLoadArgs(skill sharedLearningCatalogSkill, run string) []string {
	return []string{"self", "load", "-principal", "gpt", "-session-key", "delivery", "-run-id", run, "-target", skill.Target, "-expected-sha256", skill.SHA256, "-json"}
}

func TestSelfLoadExplicitDelivery(t *testing.T) {
	product, settings, _ := selfLearningFixture(t)
	catalog := selfLoadCatalog(t, product, settings)
	if len(catalog) != 1 {
		t.Fatalf("catalog: %+v", catalog)
	}
	skill := catalog[0]
	args := selfLoadArgs(skill, " delivery-run ")
	read := func() map[string]any {
		t.Helper()
		code, body := captureSelfLoad(t, args)
		if code != 0 {
			t.Fatalf("load exit %d", code)
		}
		var response map[string]any
		if err := json.Unmarshal(body, &response); err != nil {
			t.Fatal(err)
		}
		return response
	}
	for i := 0; i < 2; i++ {
		response := read()
		if response["status"] != "loaded" || response["run_id"] != "delivery-run" || response["rule_id"] != skill.Target+"@"+skill.SHA256 || !strings.Contains(response["context"].(string), selfLearningProcedure) {
			t.Fatalf("bad delivery: %+v", response)
		}
		path := response["context_receipt_path"].(string)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var receipt selfLearningContextReceipt
		if err := json.Unmarshal(body, &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt.Principal != "gpt" || receipt.Session != "delivery" || receipt.RunID != "delivery-run" || len(receipt.Loaded) != 1 || receipt.Loaded[0].Target != skill.Target || receipt.Loaded[0].SHA256 != skill.SHA256 || receipt.UsedAt != "" || receipt.Outcome != "" {
			t.Fatalf("bad receipt: %+v", receipt)
		}
	}
	if got := countSelfLearningEvent(t, settings.RuntimeRoot, "skill_loaded"); got != 1 {
		t.Fatalf("loaded events=%d", got)
	}
	if got := countSelfLearningEvent(t, settings.RuntimeRoot, "procedure_used"); got != 0 {
		t.Fatalf("used events=%d", got)
	}
	for _, bad := range [][]string{args[:len(args)-2], append(append([]string{}, args...), "extra"), append(append([]string{}, args[:len(args)-3]...), "wrong", "-json")} {
		if code := cmdLearn(bad); code == 0 {
			t.Fatalf("accepted invalid args: %v", bad)
		}
	}
}

func TestSelfLoadSelectsBeyondEightAndRejectsDisabledOwner(t *testing.T) {
	product, settings, _ := selfLearningFixture(t)
	for i := 0; i < 9; i++ {
		if _, err := executeSharedLearning(product, settings, "propose", map[string]string{
			"proposal_id": fmt.Sprintf("LP-extra-%d", i), "kind": "procedure",
			"target": fmt.Sprintf("skills/learned/extra-%02d.md", i), "pre_sha256": "",
			"content": selfLearningProcedure + fmt.Sprintf("\nExtra %d.\n", i),
		}); err != nil {
			t.Fatal(err)
		}
	}
	catalog := selfLoadCatalog(t, product, settings)
	if len(catalog) != 10 {
		t.Fatalf("catalog: %+v", catalog)
	}
	skill := catalog[0]
	ordered, err := prioritizeSharedSkills(settings.RuntimeRoot, catalog)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if ordered[i].Target == skill.Target {
			t.Fatalf("selected target in first eight: position %d", i)
		}
	}
	args := []string{"load", "-principal", "gpt", "-session-key", "many", "-run-id", "many-run", "-target", skill.Target, "-expected-sha256", skill.SHA256, "-json"}
	code, body := captureSelfLoad(t, append([]string{"self"}, args...))
	var response struct {
		Context string `json:"context"`
	}
	if err := json.Unmarshal(body, &response); code != 0 || err != nil || !strings.Contains(response.Context, selfLearningProcedure) {
		t.Fatalf("explicit old target: exit=%d err=%v", code, err)
	}
	wrong := append([]string{}, args...)
	wrong[len(wrong)-2] = strings.Repeat("0", 64)
	if code := cmdLearnSelf(wrong); code == 0 {
		t.Fatal("accepted stale SHA")
	}
	missing := append([]string{}, args...)
	missing[8] = "skills/learned/absent.md"
	if code := cmdLearnSelf(missing); code == 0 {
		t.Fatal("accepted absent target")
	}
	if code := cmdLearnSelf([]string{"disable", "-json"}); code != 0 {
		t.Fatal("disable failed")
	}
	if code := cmdLearnSelf(args); code == 0 {
		t.Fatal("disabled owner delivered")
	}
}

func TestSelfLoadNoFitIsNotLoaded(t *testing.T) {
	product, settings, _ := selfLearningFixture(t)
	content := selfLearningProcedure + strings.Repeat("x", 17000)
	if _, err := executeSharedLearning(product, settings, "propose", map[string]string{
		"proposal_id": "LP-oversize", "kind": "procedure", "target": "skills/learned/oversize.md", "pre_sha256": "", "content": content,
	}); err != nil {
		t.Fatal(err)
	}
	index, err := executeSharedLearning(product, settings, "index", nil)
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Skills []sharedLearningCatalogSkill `json:"skills"`
	}
	if err := json.Unmarshal(index.Data, &catalog); err != nil {
		t.Fatal(err)
	}
	for _, skill := range catalog.Skills {
		if skill.Target == "skills/learned/oversize.md" {
			if code := cmdLearnSelf([]string{"load", "-principal", "gpt", "-session-key", "oversize", "-run-id", "oversize-run", "-target", skill.Target, "-expected-sha256", skill.SHA256, "-json"}); code == 0 {
				t.Fatal("oversize context reported loaded")
			}
			if got := countSelfLearningEvent(t, settings.RuntimeRoot, "skill_loaded"); got != 0 {
				t.Fatalf("loaded events=%d", got)
			}
			return
		}
	}
	t.Fatal("oversize procedure missing from catalog")
}

func TestSelfLoadRejectsInputsBeforeUse(t *testing.T) {
	product, settings, stateDir := selfLearningFixture(t)
	skill := selfLoadCatalog(t, product, settings)[0]
	args := selfLoadArgs(skill, "reject-run")
	foreign := filepath.Join(t.TempDir(), "foreign.md")
	if err := os.WriteFile(foreign, []byte(selfLearningProcedure), 0o600); err != nil {
		t.Fatal(err)
	}
	untracked := filepath.Join(product, "skills", "learned", "untracked.md")
	if err := os.WriteFile(untracked, []byte(selfLearningProcedure), 0o600); err != nil {
		t.Fatal(err)
	}
	change := func(index int, value string) []string {
		out := append([]string{}, args...)
		out[index] = value
		return out
	}
	cases := map[string][]string{
		"untracked":         change(9, "skills/learned/untracked.md"),
		"traversal":         change(9, "../foreign.md"),
		"absolute foreign":  change(9, foreign),
		"invalid principal": change(3, "bad/principal"),
		"invalid session":   change(5, "bad/session"),
		"invalid run":       change(7, "bad/run"),
		"unknown option":    append(append([]string{}, args...), "-unknown"),
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			code, body := captureSelfLoad(t, bad)
			if code == 0 || len(body) != 0 {
				t.Fatalf("accepted %s: code=%d response=%s", name, code, body)
			}
		})
	}
	if entries, err := os.ReadDir(filepath.Join(stateDir, "air-worker-self-learning")); err == nil && len(entries) != 0 {
		t.Fatalf("rejected loads created context namespaces: %v", entries)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, kind := range []string{"skill_loaded", "procedure_used"} {
		if got := countSelfLearningEvent(t, settings.RuntimeRoot, kind); got != 0 {
			t.Fatalf("%s events=%d", kind, got)
		}
	}
}

func TestSelfLoadRejectsStaleDiskCatalog(t *testing.T) {
	product, settings, _ := selfLearningFixture(t)
	skill := selfLoadCatalog(t, product, settings)[0]
	if err := os.WriteFile(filepath.Join(product, filepath.FromSlash(skill.Target)), []byte(selfLearningProcedure+"\nchanged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, body := captureSelfLoad(t, selfLoadArgs(skill, "stale-run"))
	if code == 0 || len(body) != 0 {
		t.Fatalf("stale disk accepted: code=%d response=%s", code, body)
	}
	in := hookInput{Principal: "gpt", SessionID: "delivery", RunID: "stale-run"}
	path, err := selfLearningContextReceiptPath(in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stale current receipt: %v", err)
	}
	if got := countSelfLearningEvent(t, settings.RuntimeRoot, "procedure_used"); got != 0 {
		t.Fatalf("used events=%d", got)
	}
}

func TestSelfLoadReceiptAndOutputFailures(t *testing.T) {
	t.Run("receipt", func(t *testing.T) {
		product, settings, _ := selfLearningFixture(t)
		skill := selfLoadCatalog(t, product, settings)[0]
		in := hookInput{Principal: "gpt", SessionID: "delivery", RunID: "blocked-run"}
		path, err := selfLearningContextReceiptPath(in)
		if err != nil {
			t.Fatal(err)
		}
		block := filepath.Dir(path)
		if err := os.MkdirAll(filepath.Dir(block), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(block, []byte("block"), 0o600); err != nil {
			t.Fatal(err)
		}
		code, body := captureSelfLoad(t, selfLoadArgs(skill, "blocked-run"))
		if code == 0 || len(body) != 0 {
			t.Fatalf("receipt failure claimed success: code=%d response=%s", code, body)
		}
		if _, err := os.ReadFile(path); err == nil {
			t.Fatal("current receipt exists")
		}
		if got := countSelfLearningEvent(t, settings.RuntimeRoot, "procedure_used"); got != 0 {
			t.Fatalf("used events=%d", got)
		}
	})
	t.Run("stdout", func(t *testing.T) {
		product, settings, _ := selfLearningFixture(t)
		skill := selfLoadCatalog(t, product, settings)[0]
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		r.Close()
		w.Close()
		before := os.Stdout
		os.Stdout = w
		code := cmdLearn(selfLoadArgs(skill, "closed-run"))
		os.Stdout = before
		if code == 0 {
			t.Fatal("closed stdout claimed success")
		}
		in := hookInput{Principal: "gpt", SessionID: "delivery", RunID: "closed-run"}
		if _, _, exists, err := readSelfLearningContextReceipt(in); err != nil || !exists {
			t.Fatalf("pre-output context receipt: exists=%v err=%v", exists, err)
		}
		if got := countSelfLearningEvent(t, settings.RuntimeRoot, "procedure_used"); got != 0 {
			t.Fatalf("used events=%d", got)
		}
	})
}

func TestSelfLoadRefreshReplacesSnapshot(t *testing.T) {
	product, settings, _ := selfLearningFixture(t)
	first := selfLoadCatalog(t, product, settings)[0]
	if _, err := executeSharedLearning(product, settings, "propose", map[string]string{
		"proposal_id": "LP-refresh", "kind": "procedure", "target": "skills/learned/refresh.md", "pre_sha256": "", "content": selfLearningProcedure + "\nRefresh.\n",
	}); err != nil {
		t.Fatal(err)
	}
	var second sharedLearningCatalogSkill
	for _, skill := range selfLoadCatalog(t, product, settings) {
		if skill.Target == "skills/learned/refresh.md" {
			second = skill
		}
	}
	if second.Target == "" {
		t.Fatal("refresh target missing")
	}
	code, _ := captureSelfLoad(t, selfLoadArgs(first, "refresh-run"))
	if code != 0 {
		t.Fatalf("first load exit %d", code)
	}
	in := hookInput{Principal: "gpt", SessionID: "delivery", RunID: "refresh-run"}
	rec, path, exists, err := readSelfLearningContextReceipt(in)
	if err != nil || !exists {
		t.Fatalf("first receipt: %v exists=%v", err, exists)
	}
	rec.UsedAt, rec.UsedRuleID, rec.Outcome, rec.OutcomeRef, rec.ObservedTool = "old", "old", "old", "old", "old"
	if err := writeSelfLearningContextReceipt(path, rec); err != nil {
		t.Fatal(err)
	}
	code, body := captureSelfLoad(t, selfLoadArgs(second, "refresh-run"))
	if code != 0 {
		t.Fatalf("refresh exit %d: %s", code, body)
	}
	rec, _, exists, err = readSelfLearningContextReceipt(in)
	if err != nil || !exists || len(rec.Loaded) != 1 || rec.Loaded[0].Target != second.Target || rec.UsedAt != "" || rec.UsedRuleID != "" || rec.Outcome != "" || rec.OutcomeRef != "" || rec.ObservedTool != "" {
		t.Fatalf("stale current snapshot: %+v exists=%v err=%v", rec, exists, err)
	}
	if got := countSelfLearningEvent(t, settings.RuntimeRoot, "procedure_used"); got != 0 {
		t.Fatalf("used events=%d", got)
	}
}
