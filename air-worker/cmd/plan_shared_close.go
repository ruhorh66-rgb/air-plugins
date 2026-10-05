package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Testable boundary: fault-injection checks change files while the actual
// module preflight is in progress. Production always invokes the shared core.
var sharedPlanPreflight = func(root string, s sharedLearningSettings) error {
	_, err := executeSharedLearning(root, s, "status", nil)
	return err
}

// The outbox belongs to the plan transaction, not a second learning journal.
// Once published, its original actor, receipt, time and event identity survive
// retries and process restarts. Only the shared module writes learning events.
type sharedPlanCloseIntent struct {
	Schema      string   `json:"schema"`
	Product     string   `json:"product"`
	ProductID   string   `json:"product_id"`
	RuntimeRoot string   `json:"runtime_root"`
	Phase       string   `json:"phase"`
	BeforeSHA   string   `json:"before_sha256"`
	AfterSHA    string   `json:"after_sha256"`
	Node        planNode `json:"node"`
	Body        string   `json:"body"`
	Receipt     string   `json:"receipt"`
	Actor       string   `json:"actor"`
}

func persistSharedPlanClose(path string, intent sharedPlanCloseIntent) error {
	b, err := json.MarshalIndent(intent, "", "  ")
	if err != nil {
		return err
	}
	if len(b) > 2*sharedLearningMaxBytes {
		return errors.New("plan close intent exceeds byte limit")
	}
	return writeFileAtomic(path, append(b, '\n'))
}

// The caller holds the product's plan-nodes lock. A module error can occur AFTER
// the event/procedure committed, so it must never trigger blind plan rollback.
func cmdSharedPlanNodeClose(root string, s sharedLearningSettings, n planNode, body, path, receipt, actor string, before []byte, asJSON bool) int {
	receipt = strings.TrimSpace(receipt)
	intentPath := filepath.Join(s.RuntimeRoot, "plan-close", learnSHA([]byte(n.ID+"\n"+n.CreatedAt))+".json")
	fail := func(err error) int {
		if asJSON {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"schema": "air-worker.plan.node.mutation/v1", "action": "pending", "node_id": n.ID, "error": err.Error(), "recovery_ref": intentPath})
		} else {
			fmt.Fprintln(os.Stderr, "plan close requires reconciliation:", err)
		}
		return 2
	}
	var intent sharedPlanCloseIntent
	b, err := readLearningBounded(intentPath, 2*sharedLearningMaxBytes)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fail(err)
	}
	if errors.Is(err, os.ErrNotExist) {
		// A historical closed node is not a newly completed run at cutover.
		if n.Status == "closed" {
			if asJSON {
				_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"schema": "air-worker.plan.node.mutation/v1", "action": "already_closed", "node": n, "path": path})
			} else {
				fmt.Printf("%s already closed%s", n.ID, lineEnding)
			}
			return 0
		}
		// Verify module identity/ownership and initial I/O before publishing a
		// close intent or changing the plan. Only a later failure is recoverable.
		if err := sharedPlanPreflight(root, s); err != nil {
			return fail(err)
		}
		currentBefore, err := os.ReadFile(path)
		if err != nil {
			return fail(err)
		}
		if learnSHA(currentBefore) != learnSHA(before) {
			return fail(errors.New("node changed during close preflight; edit retained"))
		}
		closed := n
		closed.Receipts = append([]string(nil), n.Receipts...)
		closed.Status = "closed"
		closed.UpdatedAt = planNodeNow().UTC().Format(time.RFC3339Nano)
		found := false
		for _, ref := range closed.Receipts {
			if ref == receipt {
				found = true
			}
		}
		if !found {
			closed.Receipts = append(closed.Receipts, receipt)
		}
		intent = sharedPlanCloseIntent{Schema: "air-worker.plan.close-intent/v1", Product: root, ProductID: s.ProductID, RuntimeRoot: s.RuntimeRoot, Phase: "prepared", BeforeSHA: learnSHA(before), AfterSHA: learnSHA(renderPlanNodeWithBody(closed, body)), Node: closed, Body: body, Receipt: receipt, Actor: actor}
		if err := os.MkdirAll(filepath.Dir(intentPath), 0700); err != nil {
			return fail(err)
		}
		if err := persistSharedPlanClose(intentPath, intent); err != nil {
			return fail(err)
		}
	} else if err := json.Unmarshal(b, &intent); err != nil {
		return fail(err)
	}

	if intent.Schema != "air-worker.plan.close-intent/v1" || intent.Product != root || intent.ProductID != s.ProductID || intent.RuntimeRoot != s.RuntimeRoot || intent.Node.ID != n.ID || intent.Node.CreatedAt != n.CreatedAt || intent.Node.Status != "closed" || intent.Receipt != receipt || intent.Actor == "" || len(intent.BeforeSHA) != 64 || (intent.Phase != "prepared" && intent.Phase != "complete") {
		return fail(errors.New("close intent identity/receipt conflict; original intent retained"))
	}
	if err := validatePlanNode(intent.Node); err != nil {
		return fail(err)
	}
	closedAt, err := time.Parse(time.RFC3339Nano, intent.Node.UpdatedAt)
	if err != nil {
		return fail(err)
	}
	after := renderPlanNodeWithBody(intent.Node, intent.Body)
	if learnSHA(after) != intent.AfterSHA {
		return fail(errors.New("close intent snapshot SHA mismatch"))
	}
	current, err := os.ReadFile(path)
	if err != nil {
		return fail(err)
	}
	currentSHA := learnSHA(current)
	if currentSHA != intent.BeforeSHA && currentSHA != intent.AfterSHA {
		return fail(errors.New("node changed outside pending close; refusing to overwrite it"))
	}
	if intent.Phase == "complete" && currentSHA != intent.AfterSHA {
		return fail(errors.New("completed node was reverted outside core"))
	}
	if intent.Phase != "complete" {
		if currentSHA != intent.AfterSHA {
			if err := writeFileAtomic(path, after); err != nil {
				return fail(err)
			}
		}
		// Recompute only this spine link against the CURRENT plan. Never replace a
		// saved whole PLAN over unrelated nodes added while recovery was pending.
		if err := updateNodeInSpine(filepath.Join(root, "PLAN.md"), intent.Node); err != nil {
			return fail(err)
		}
		event := planNodeEventRowWithActor(intent.Node, "closed", intent.Receipt, "plan-node", closedAt, intent.Actor)
		if err := appendPlanNodeEvents(root, []map[string]any{event}); err != nil {
			return fail(err)
		}
		// Duplicate observe can have a completed event even when its trace failed.
		// A fresh successful native operation proves this tail is writable again.
		if _, err := executeSharedLearning(root, s, "status", nil); err != nil {
			return fail(err)
		}
		intent.Phase = "complete"
		if err := persistSharedPlanClose(intentPath, intent); err != nil {
			return fail(err)
		}
	}
	if asJSON {
		if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"schema": "air-worker.plan.node.mutation/v1", "action": "closed", "actor": intent.Actor, "node": intent.Node, "path": path, "recovery_ref": intentPath}); err != nil {
			return 2
		}
	} else {
		fmt.Printf("%s closed -> %s%s", intent.Node.ID, intent.Node.ReturnTo, lineEnding)
	}
	return 0
}
