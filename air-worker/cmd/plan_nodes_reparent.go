package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const planReparentSchema = "air-worker.plan.node.reparent-intent/v1"

type planReparentIntent struct {
	Schema      string `json:"schema"`
	RequestID   string `json:"request_id"`
	NodeID      string `json:"node_id"`
	Original    string `json:"original_parent"`
	Destination string `json:"destination_parent"`
	Actor       string `json:"actor"`
	Receipt     string `json:"receipt"`
	ReceiptSHA  string `json:"receipt_sha256"`
	ChangedAt   string `json:"changed_at"`
	BeforeNode  string `json:"before_node_sha256"`
	BeforePlan  string `json:"before_plan_sha256"`
	AfterNode   string `json:"after_node_sha256"`
	AfterPlan   string `json:"after_plan_sha256"`
	Phase       string `json:"phase"`
}

func persistPlanReparent(path string, record planReparentIntent) error {
	body, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return writeFileAtomicDurable(path, append(body, byte(10)))
}

func plannedReparentText(oldPlan []byte, before, after planNode) ([]byte, error) {
	raw := strings.ReplaceAll(string(oldPlan), string([]byte{13, 10}), string(byte(10)))
	line := spineNodeLine(before)
	if strings.Count(raw, line) != 1 || strings.Count(raw, "[["+before.ID+"]]") != 1 {
		return nil, fmt.Errorf("original PLAN spine has no unique exact node link for %s", before.ID)
	}
	return []byte(strings.Replace(raw, line, spineNodeLine(after), 1)), nil
}

func cmdPlanNodeReparent(argv []string) int {
	if len(argv) == 0 || strings.HasPrefix(argv[0], "-") {
		fmt.Fprintln(os.Stderr, "usage: air-worker plan node reparent <id> -product <root> -from <expected-parent> -to <existing-parent> -receipt <evidence> -request-id <key>")
		return 2
	}
	id := argv[0]
	fs := flag.NewFlagSet("plan node reparent", flag.ContinueOnError)
	product := fs.String("product", ".", "product root")
	from := fs.String("from", "", "exact expected former parent")
	to := fs.String("to", "", "exact existing destination parent")
	receipt := fs.String("receipt", "", "immutable evidence document")
	req := fs.String("request-id", "", "one stable transaction key")
	actorKind := fs.String("actor-kind", "", "gpt-window or claude-session")
	actor := fs.String("actor", "", "named actor/session")
	asJSON := fs.Bool("json", false, "machine-readable JSON")
	if err := fs.Parse(argv[1:]); err != nil {
		return 2
	}
	if fs.NArg() != 0 || strings.TrimSpace(*from) == "" || strings.TrimSpace(*to) == "" ||
		strings.TrimSpace(*receipt) == "" || len(strings.TrimSpace(*req)) < 8 ||
		len(strings.TrimSpace(*req)) > 256 || *from == *to {
		fmt.Fprintln(os.Stderr, "reparent requires exact distinct from/to, receipt and stable request-id (8..256 bytes)")
		return 2
	}
	root, err := normalizePlanNodeProduct(*product)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	lock, ok := acquireLock(lockName("plan-nodes", root))
	if !ok {
		fmt.Fprintln(os.Stderr, "plan node mutation already running")
		return 1
	}
	defer lock.release()
	path, err := planNodePathByID(root, id)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	nodeRaw, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	node, err := parsePlanNode(path, nodeRaw)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	body, err := parsePlanNodeBody(path, nodeRaw)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if node.Status != "open" || node.Owner == "" || node.ID != id {
		fmt.Fprintln(os.Stderr, "reparent requires an exact owned OPEN node ID")
		return 2
	}
	if err := validateExistingPlanParent(root, *to, node.ID); err != nil {
		fmt.Fprintln(os.Stderr, "reparent target:", err)
		return 2
	}
	if !strings.HasPrefix(*to, "N-") {
		fmt.Fprintln(os.Stderr, "reparent destination must be an exact existing N-* node")
		return 2
	}
	eventActor, err := resolveMutationActor(*actorKind, *actor, node.Owner)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	absEvidence, err := filepath.Abs(*receipt)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	info, err := os.Lstat(absEvidence)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() < 2 || info.Size() > 256*1024 {
		fmt.Fprintln(os.Stderr, "invalid regular bounded evidence receipt")
		return 2
	}
	evidence, err := os.ReadFile(absEvidence)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	evidenceSHA := learnSHA(evidence)
	settings, shared, err := readSharedLearningSettings(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	journalDir := filepath.Join(root, ".air-worker", "plan-reparent")
	if shared {
		journalDir = filepath.Join(settings.RuntimeRoot, "plan-reparent")
	}
	journal := filepath.Join(journalDir, learnSHA([]byte(strings.TrimSpace(*req)))+".json")
	var prior planReparentIntent
	priorBytes, loadErr := readLearningBounded(journal, sharedLearningMaxBytes)
	existing := loadErr == nil
	if loadErr != nil && !errors.Is(loadErr, os.ErrNotExist) {
		fmt.Fprintln(os.Stderr, loadErr)
		return 2
	}
	if existing {
		if err := json.Unmarshal(priorBytes, &prior); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		if prior.Schema != planReparentSchema || prior.RequestID != *req || prior.NodeID != node.ID ||
			prior.Original != *from || prior.Destination != *to || prior.Actor != eventActor ||
			prior.Receipt != absEvidence || prior.ReceiptSHA != evidenceSHA {
			fmt.Fprintln(os.Stderr, "reparent idempotency key is bound to different actor/receipt/plan")
			return 2
		}
		if prior.Phase == "complete" {
			plan, readErr := os.ReadFile(filepath.Join(root, "PLAN.md"))
			if readErr != nil || node.Parent != *to ||
				strings.Count(string(plan), spineNodeLine(node)) != 1 {
				fmt.Fprintln(os.Stderr, "completed reparent receipt differs from actual native plan state")
				return 2
			}
			if *asJSON {
				_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"action": "already_reparented", "node_id": id, "request_id": *req, "receipt": journal, "status": "PASS"})
			}
			return 0
		}
		if prior.Phase != "prepared" {
			fmt.Fprintln(os.Stderr, "incomplete reparent journal requires manual reconciliation; refusing duplicate LEARN event")
			return 2
		}
	}
	if node.Parent != *from {
		fmt.Fprintln(os.Stderr, "current node parent differs from expected former parent")
		return 2
	}
	planPath := filepath.Join(root, "PLAN.md")
	beforePlan, err := os.ReadFile(planPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if shared {
		if err := sharedPlanPreflight(root, settings); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	after := node
	after.Parent = *to
	if after.ReturnTo == *from {
		after.ReturnTo = *to
	}
	timestamp := time.Now().UTC()
	if existing {
		timestamp, err = time.Parse(time.RFC3339Nano, prior.ChangedAt)
		if err != nil {
			fmt.Fprintln(os.Stderr, "invalid pending intent timestamp:", err)
			return 2
		}
	}
	after.UpdatedAt = timestamp.Format(time.RFC3339Nano)
	if err := validatePlanNode(after); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	afterNode := renderPlanNodeWithBody(after, body)
	afterPlan, err := plannedReparentText(beforePlan, node, after)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	expectedIntent := planReparentIntent{
		Schema: planReparentSchema, RequestID: *req, NodeID: id,
		Original: *from, Destination: *to, Actor: eventActor, Receipt: absEvidence,
		ReceiptSHA: evidenceSHA, ChangedAt: after.UpdatedAt,
		BeforeNode: learnSHA(nodeRaw), BeforePlan: learnSHA(beforePlan),
		AfterNode: learnSHA(afterNode), AfterPlan: learnSHA(afterPlan), Phase: "prepared",
	}
	if existing {
		if prior.BeforeNode != expectedIntent.BeforeNode || prior.BeforePlan != expectedIntent.BeforePlan ||
			prior.AfterNode != expectedIntent.AfterNode || prior.AfterPlan != expectedIntent.AfterPlan {
			fmt.Fprintln(os.Stderr, "pending reparent has incompatible document hashes")
			return 2
		}
	} else if err := persistPlanReparent(journal, expectedIntent); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := writeFileAtomicDurable(path, afterNode); err != nil {
		fmt.Fprintln(os.Stderr, "node publish failed:", err)
		return 2
	}
	if err := writeFileAtomicDurable(planPath, afterPlan); err != nil {
		_ = writeFileAtomicDurable(path, nodeRaw)
		fmt.Fprintln(os.Stderr, "PLAN publish failed:", err)
		return 2
	}
	checkNode, nodeErr := os.ReadFile(path)
	checkPlan, planErr := os.ReadFile(planPath)
	if nodeErr != nil || planErr != nil || learnSHA(checkNode) != expectedIntent.AfterNode || learnSHA(checkPlan) != expectedIntent.AfterPlan {
		_ = writeFileAtomicDurable(path, nodeRaw)
		_ = writeFileAtomicDurable(planPath, beforePlan)
		fmt.Fprintln(os.Stderr, "reparent publish lacks exact durable machine readback")
		return 2
	}
	expectedIntent.Phase = "event_pending"
	if err := persistPlanReparent(journal, expectedIntent); err != nil {
		fmt.Fprintln(os.Stderr, "reparent published without safe event intent:", err)
		return 2
	}
	event := planNodeEventRowWithActor(after, "reparented", absEvidence+"#sha256="+evidenceSHA, "plan-node", timestamp, eventActor)
	if err := appendPlanNodeEvents(root, []map[string]any{event}); err != nil {
		fmt.Fprintln(os.Stderr, "reparent written but event unconfirmed; journal requires reconciliation:", err)
		return 2
	}
	expectedIntent.Phase = "complete"
	if err := persistPlanReparent(journal, expectedIntent); err != nil {
		fmt.Fprintln(os.Stderr, "reparent event written, but final intent not durable:", err)
		return 2
	}
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"schema": planReparentSchema, "action": "reparented", "node_id": id,
			"from": *from, "to": *to, "status": "PASS", "request_id": *req,
			"journal": journal, "node_sha256": expectedIntent.AfterNode,
			"plan_sha256": expectedIntent.AfterPlan, "receipt_sha256": evidenceSHA,
		})
	} else {
		fmt.Println("reparented", id, "->", *to)
	}
	return 0
}
