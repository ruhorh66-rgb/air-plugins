package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func prepareRound4PendingMigration(t *testing.T) (string, sharedLearningSettings, []planNode) {
	t.Helper()
	root, s := prepareSharedPlanCreateFixture(t)
	f, err := os.OpenFile(filepath.Join(root, "PLAN.md"), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("\n## first section\nFirst body is retained.\n\n## second section\nSecond body is retained.\n")
	closeErr := f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	t.Setenv("AW_SHARED_PLAN_CREATE_LOG_FAULT", "1")
	args := sharedPlanCreateArgs("migrate", root, "round4-pending-migration")
	code, out := runSharedPlanChild(t, "migrate", args)
	if code == 0 {
		t.Fatalf("fixture did not leave a pending migration: %s", out)
	}
	if err := os.Remove(filepath.Join(s.RuntimeRoot, "logs")); err != nil {
		t.Fatal(err)
	}
	nodes, err := listPlanNodes(root)
	if err != nil || len(nodes) != 2 {
		t.Fatalf("pending migration fixture nodes=%d err=%v out=%s", len(nodes), err, out)
	}
	return root, s, nodes
}

func TestRound4PendingMigrationBlocksInterleavedMutations(t *testing.T) {
	for _, action := range []string{"new", "close"} {
		t.Run(action, func(t *testing.T) {
			root, s, nodes := prepareRound4PendingMigration(t)
			beforePlan, err := os.ReadFile(filepath.Join(root, "PLAN.md"))
			if err != nil {
				t.Fatal(err)
			}
			beforeEvents := sharedEventRows(t, s)

			var code int
			var out []byte
			switch action {
			case "new":
				code, out = runSharedPlanChild(t, "new", sharedPlanCreateArgs("new", root, "round4-competing-new"))
			case "close":
				args := []string{nodes[0].ID, "-product", root, "-receipt", "round4/competing-close.json", "-actor", "second-consumer", "-actor-kind", "gpt-window", "-json"}
				code, out = runSharedPlanChild(t, "close", args)
			}
			if code == 0 {
				t.Fatalf("competing %s was accepted while migration intent is prepared: %s", action, out)
			}
			if !strings.Contains(strings.ToLower(string(out)), "pending") {
				t.Fatalf("competing %s failed without naming pending transaction: %s", action, out)
			}
			afterPlan, err := os.ReadFile(filepath.Join(root, "PLAN.md"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(beforePlan, afterPlan) {
				t.Fatalf("competing %s changed PLAN while another transaction was pending", action)
			}
			afterEvents := sharedEventRows(t, s)
			if len(afterEvents) != len(beforeEvents) {
				t.Fatalf("competing %s emitted events while another transaction was pending: before=%d after=%d", action, len(beforeEvents), len(afterEvents))
			}
		})
	}
}

func TestRound4UnpublishedIDReservationBlocksOtherNew(t *testing.T) {
	root, s := prepareSharedPlanCreateFixture(t)
	before, err := os.ReadFile(filepath.Join(root, "PLAN.md"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	req := sharedPlanCreateRequest{
		ID: "round4-reserved-request", Operation: "new", Title: "reserved task",
		Parent: "reserved stage", Owner: "first-consumer", DoneWhen: "reserved task completes",
		Trigger: "round4 unpublished reservation", ReturnTo: "reserved stage",
	}
	requestBytes, _ := json.Marshal(req)
	n := planNode{
		ID: "N-001_reserved-task", Title: req.Title, Parent: req.Parent, Owner: req.Owner,
		DoneWhen: req.DoneWhen, Trigger: req.Trigger, Status: "open", ReturnTo: req.ReturnTo,
		Receipts: []string{}, CreatedAt: now, UpdatedAt: now,
	}
	content := renderPlanNode(n)
	intent := sharedPlanCreateIntent{
		Schema: "air-worker.plan.create-intent/v1", Product: root, ProductID: s.ProductID,
		RuntimeRoot: s.RuntimeRoot, Request: req, RequestSHA: learnSHA(requestBytes),
		Actor: "gpt-window:first-consumer", Phase: "prepared", BeforePlanSHA: learnSHA(before),
		Nodes: []sharedPlanCreateNode{{Node: n, Content: content, SHA: learnSHA(content), Ref: filepath.ToSlash(filepath.Join(planNodeDirName, n.ID+".md"))}},
	}
	intentPath := filepath.Join(s.RuntimeRoot, "plan-create", learnSHA([]byte(req.Operation+"\n"+req.ID))+".json")
	if err := os.MkdirAll(filepath.Dir(intentPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := persistSharedPlanCreate(intentPath, intent); err != nil {
		t.Fatal(err)
	}
	if nodes, err := listPlanNodes(root); err != nil || len(nodes) != 0 {
		t.Fatalf("reservation fixture unexpectedly published nodes: %v %v", nodes, err)
	}

	code, out := runSharedPlanChild(t, "new", sharedPlanCreateArgs("new", root, "round4-other-request"))
	if code == 0 {
		t.Fatalf("new request ignored unpublished ID reservation: %s", out)
	}
	if !strings.Contains(strings.ToLower(string(out)), "pending") {
		t.Fatalf("reservation conflict did not name pending transaction: %s", out)
	}
	if nodes, err := listPlanNodes(root); err != nil || len(nodes) != 0 {
		t.Fatalf("competing request published a node despite reservation: %v %v", nodes, err)
	}
	after, err := os.ReadFile(filepath.Join(root, "PLAN.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("competing request changed PLAN despite unpublished reservation")
	}
}

func TestRound4IntentPersistenceUsesDurableAtomicWriter(t *testing.T) {
	for _, name := range []string{"plan_shared_create.go", "plan_shared_close.go"} {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		text := string(b)
		start := strings.Index(text, "func persistSharedPlan")
		if start < 0 {
			t.Fatalf("%s: persistence function missing", name)
		}
		tail := text[start:]
		end := strings.Index(tail, "\n}\n")
		if end < 0 {
			t.Fatalf("%s: persistence function boundary missing", name)
		}
		body := tail[:end]
		if !strings.Contains(body, "writeFileAtomicDurable") {
			t.Fatalf("%s: plan intent is not published through flushed atomic writer", name)
		}
	}
}

func TestRound4OversizedSummaryNeverStartsAdapter(t *testing.T) {
	product, s := sharedProductFixture(t, true)
	marker := filepath.Join(t.TempDir(), "adapter-started")
	t.Setenv("AW_LEARNING_START_MARKER", marker)
	s.DeliverSummary = learningAdapterFixture(t, "launch-marker")
	if err := os.MkdirAll(s.RuntimeRoot, 0700); err != nil {
		t.Fatal(err)
	}

	var proposals bytes.Buffer
	padding := strings.Repeat("x", 220)
	for i := 0; i < 900; i++ {
		row := map[string]string{
			"schema": "air.learning.proposal/v1", "proposal_id": fmt.Sprintf("LP-round4-%04d", i),
			"kind": "check_spec", "target": "rules/" + padding + fmt.Sprintf("-%04d.json", i),
			"diff_ref":    "diff/" + padding + fmt.Sprintf("-%04d.patch", i),
			"diff_sha256": strings.Repeat("a", 64),
		}
		b, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		proposals.Write(b)
		proposals.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(s.RuntimeRoot, "proposals.jsonl"), proposals.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}

	got, err := executeSharedLearning(product, s, "summary", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "pending_delivery" {
		t.Fatalf("oversized summary status=%s data=%s", got.Status, got.Data)
	}
	if !strings.Contains(strings.ToLower(string(got.Data)), "input exceeds byte limit") {
		t.Fatalf("pending delivery did not expose bounded-input error: %s", got.Data)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("oversized input started delivery adapter: %v", err)
	}
}
