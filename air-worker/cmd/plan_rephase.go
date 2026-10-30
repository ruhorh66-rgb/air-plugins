package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	planRephaseRequestSchema   = "air-worker.plan-rephase-request/v1"
	planRephaseHistorySchema   = "air-worker.plan-rephase-history-evidence/v1"
	planRephaseAuthoritySchema = "air-worker.plan-rephase-authority/v1"
	planRephaseIntentSchema    = "air-worker.plan-rephase-intent/v1"
	planRephaseReceiptSchema   = "air-worker.plan-rephase-receipt/v1"
	planRephaseMaxEvidence     = 256 * 1024
)

var (
	planRephaseSHA40   = regexp.MustCompile(`^[0-9a-f]{40}$`)
	planRephaseSHA256  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	planRephaseEventID = regexp.MustCompile(`^EV-[0-9a-f]{24}$`)
	planRephaseRef     = regexp.MustCompile(`^refs/heads/[A-Za-z0-9][A-Za-z0-9._/-]{0,180}$`)
)

type planRephaseRequest struct {
	Schema                 string             `json:"schema"`
	Repository             string             `json:"repository"`
	RemoteName             string             `json:"remote_name"`
	CanonicalRef           string             `json:"canonical_ref"`
	ExpectedHead           string             `json:"expected_head"`
	AcceptedAncestor       string             `json:"accepted_ancestor"`
	Generation             string             `json:"generation"`
	HistoryManifestSHA256  string             `json:"history_manifest_sha256"`
	HistoryManifestPath    string             `json:"history_manifest_path"`
	SourceProvenanceRef    string             `json:"source_provenance_ref"`
	SourceProvenanceCommit string             `json:"source_provenance_commit"`
	Node                   planRephaseNode    `json:"node"`
	Plan                   planRephasePlan    `json:"plan"`
	Release                planRephaseRelease `json:"release"`
}

type planRephaseNode struct {
	ID             string `json:"id"`
	ExpectedSHA256 string `json:"expected_sha256"`
	ExpectedOwner  string `json:"expected_owner"`
	BeforeParent   string `json:"before_parent"`
	AfterParent    string `json:"after_parent"`
	BeforeReturnTo string `json:"before_return_to"`
	AfterReturnTo  string `json:"after_return_to"`
	BeforeTrigger  string `json:"before_trigger"`
	AfterTrigger   string `json:"after_trigger"`
	BeforeDoneWhen string `json:"before_done_when"`
	AfterDoneWhen  string `json:"after_done_when"`
}

type planRephasePlan struct {
	ExpectedSHA256     string `json:"expected_sha256"`
	HeaderBefore       string `json:"header_before"`
	HeaderBeforeSHA256 string `json:"header_before_sha256"`
	HeaderAfter        string `json:"header_after"`
}

type planRephaseRelease struct {
	ExpectedSHA256 string `json:"expected_sha256"`
	Version        string `json:"version"`
	StepID         string `json:"step_id"`
	BeforeTrigger  string `json:"before_trigger"`
	AfterTrigger   string `json:"after_trigger"`
	BeforeDoneWhen string `json:"before_done_when"`
	AfterDoneWhen  string `json:"after_done_when"`
	// One scoped pair is applied to BOTH the release and its selected step.
	// Old requests without this pair retain the existing rephase behavior.
	BeforeNodeRefs []string `json:"before_node_refs,omitempty"`
	AfterNodeRefs  []string `json:"after_node_refs,omitempty"`
}

type planRephaseHistoryEvidence struct {
	Schema                 string `json:"schema"`
	Repository             string `json:"repository"`
	CanonicalRef           string `json:"canonical_ref"`
	ExpectedHead           string `json:"expected_head"`
	Generation             string `json:"generation"`
	SourceProvenanceRef    string `json:"source_provenance_ref"`
	SourceProvenanceCommit string `json:"source_provenance_commit"`
}

type planRephaseAuthority struct {
	Schema         string `json:"schema"`
	Operation      string `json:"operation"`
	Approved       bool   `json:"approved"`
	Repository     string `json:"repository"`
	RemoteName     string `json:"remote_name"`
	CanonicalRef   string `json:"canonical_ref"`
	ExpectedHead   string `json:"expected_head"`
	Generation     string `json:"generation"`
	Actor          string `json:"actor"`
	NodeID         string `json:"node_id"`
	ReleaseVersion string `json:"release_version"`
	RequestSHA256  string `json:"request_sha256"`
}

type planRephaseIntent struct {
	Schema              string `json:"schema"`
	RequestID           string `json:"request_id"`
	RequestSHA256       string `json:"request_sha256"`
	AuthoritySHA256     string `json:"authority_sha256"`
	Actor               string `json:"actor"`
	NodeID              string `json:"node_id"`
	Repository          string `json:"repository"`
	CanonicalRef        string `json:"canonical_ref"`
	ExpectedHead        string `json:"expected_head"`
	Generation          string `json:"generation"`
	BeforeNodeSHA256    string `json:"before_node_sha256"`
	AfterNodeSHA256     string `json:"after_node_sha256"`
	BeforePlanSHA256    string `json:"before_plan_sha256"`
	AfterPlanSHA256     string `json:"after_plan_sha256"`
	BeforeReleaseSHA256 string `json:"before_release_sha256"`
	AfterReleaseSHA256  string `json:"after_release_sha256"`
	SnapshotSHA256      string `json:"snapshot_sha256"`
	ReceiptSHA256       string `json:"receipt_sha256"`
	ChangedAt           string `json:"changed_at"`
	Phase               string `json:"phase"`
}

type planRephaseSnapshot struct {
	Schema        string `json:"schema"`
	RequestID     string `json:"request_id"`
	NodeFile      string `json:"node_file"`
	PlanFile      string `json:"plan_file"`
	ReleaseFile   string `json:"release_file"`
	NodeSHA256    string `json:"node_sha256"`
	PlanSHA256    string `json:"plan_sha256"`
	ReleaseSHA256 string `json:"release_sha256"`
}

type planRephaseReceipt struct {
	Schema                  string   `json:"schema"`
	Status                  string   `json:"status"`
	RequestID               string   `json:"request_id"`
	RequestSHA256           string   `json:"request_sha256"`
	AuthorityReceiptSHA256  string   `json:"authority_receipt_sha256"`
	Actor                   string   `json:"actor"`
	Repository              string   `json:"repository"`
	CanonicalRef            string   `json:"canonical_ref"`
	ObservedHead            string   `json:"observed_head"`
	ObservedRemoteHead      string   `json:"observed_remote_head"`
	AcceptedAncestor        string   `json:"accepted_ancestor"`
	Generation              string   `json:"generation"`
	HistoryManifestSHA256   string   `json:"history_manifest_sha256"`
	HistoryManifestPath     string   `json:"history_manifest_path"`
	SourceProvenanceRef     string   `json:"source_provenance_ref"`
	SourceProvenanceCommit  string   `json:"source_provenance_commit"`
	NodeID                  string   `json:"node_id"`
	StatusPreserved         bool     `json:"status_preserved"`
	ReceiptsPreserved       bool     `json:"receipts_preserved"`
	SilentClosure           bool     `json:"silent_closure"`
	ImportedHistoricalNodes int      `json:"imported_historical_nodes"`
	BeforeNodeSHA256        string   `json:"before_node_sha256"`
	AfterNodeSHA256         string   `json:"after_node_sha256"`
	BeforePlanSHA256        string   `json:"before_plan_sha256"`
	AfterPlanSHA256         string   `json:"after_plan_sha256"`
	BeforeHeaderSHA256      string   `json:"before_header_sha256"`
	AfterHeaderSHA256       string   `json:"after_header_sha256"`
	BeforeReleaseSHA256     string   `json:"before_release_sha256"`
	AfterReleaseSHA256      string   `json:"after_release_sha256"`
	BeforeParent            string   `json:"before_parent"`
	AfterParent             string   `json:"after_parent"`
	BeforeTrigger           string   `json:"before_trigger"`
	AfterTrigger            string   `json:"after_trigger"`
	BeforeDoneWhen          string   `json:"before_done_when"`
	AfterDoneWhen           string   `json:"after_done_when"`
	ReleaseVersion          string   `json:"release_version"`
	ReleaseStepID           string   `json:"release_step_id"`
	BeforeNodeRefs          []string `json:"before_node_refs,omitempty"`
	AfterNodeRefs           []string `json:"after_node_refs,omitempty"`
	RollbackSnapshot        string   `json:"rollback_snapshot"`
	RollbackSnapshotSHA256  string   `json:"rollback_snapshot_sha256"`
	ReadbackChecks          []string `json:"readback_checks"`
	NoReleaseEffect         bool     `json:"no_release_effect"`
	NoInstallEffect         bool     `json:"no_install_effect"`
	NoProviderEffect        bool     `json:"no_provider_effect"`
	ChangedAt               string   `json:"changed_at"`
}

type planRephaseIO struct {
	write func(string, []byte) error
	read  func(string) ([]byte, error)
	event func(string, []map[string]any) error
}

func defaultPlanRephaseIO() planRephaseIO {
	return planRephaseIO{write: writeFileAtomicDurable, read: os.ReadFile, event: appendPlanNodeEvents}
}

func cmdPlanRephase(argv []string) int {
	if len(argv) == 0 || argv[0] != "apply" {
		fmt.Fprintln(os.Stderr, "usage: air-worker plan rephase apply <full-id> -product <root> -manifest <request.json> -manifest-sha256 <sha> -authority-receipt <receipt.json> -authority-sha256 <sha> -request-id <key> -actor-kind <kind> -actor <actor> [-json]")
		return 2
	}
	return cmdPlanRephaseApply(argv[1:])
}

type planRephaseAuthorityVerifier func(root string, req planRephaseRequest, auth planRephaseAuthority, authorityRaw []byte, actor, requestSHA string) error

func defaultPlanRephaseAuthorityVerifier(root string, req planRephaseRequest, auth planRephaseAuthority, authorityRaw []byte, actor, requestSHA string) error {
	if err := validatePlanRephaseAuthority(auth, req, actor, requestSHA); err != nil {
		return err
	}
	return errors.New("trusted N124 authority verifier is not configured; source-only candidate must HOLD")
}

func cmdPlanRephaseApply(argv []string) int {
	return cmdPlanRephaseApplyWithVerifier(argv, defaultPlanRephaseAuthorityVerifier)
}

func cmdPlanRephaseApplyWithVerifier(argv []string, verifyAuthority planRephaseAuthorityVerifier) int {
	if len(argv) == 0 || strings.HasPrefix(argv[0], "-") {
		fmt.Fprintln(os.Stderr, "plan rephase apply requires exact full node ID")
		return 2
	}
	id := strings.TrimSpace(argv[0])
	fs := flag.NewFlagSet("plan rephase apply", flag.ContinueOnError)
	product := fs.String("product", ".", "product root")
	manifest := fs.String("manifest", "", "strict rephase request JSON")
	manifestSHA := fs.String("manifest-sha256", "", "exact request byte SHA256")
	authority := fs.String("authority-receipt", "", "trusted authority receipt JSON")
	authoritySHA := fs.String("authority-sha256", "", "exact authority receipt byte SHA256")
	requestID := fs.String("request-id", "", "stable idempotency key")
	actorKind := fs.String("actor-kind", "", "gpt-window or claude-session")
	actor := fs.String("actor", "", "named actor/session")
	asJSON := fs.Bool("json", false, "machine-readable JSON")
	if err := fs.Parse(argv[1:]); err != nil || fs.NArg() != 0 {
		return 2
	}
	if !planNodeIDRE.MatchString(id) || !strings.Contains(id, "_") {
		return planRephaseReject(*asJSON, "INVALID_FULL_NODE_ID", 2)
	}
	if len(strings.TrimSpace(*requestID)) < 8 || len(strings.TrimSpace(*requestID)) > 256 {
		return planRephaseReject(*asJSON, "INVALID_REQUEST_ID", 2)
	}
	if !planRephaseSHA256.MatchString(strings.TrimSpace(*manifestSHA)) || !planRephaseSHA256.MatchString(strings.TrimSpace(*authoritySHA)) {
		return planRephaseReject(*asJSON, "INVALID_EVIDENCE_SHA256", 2)
	}
	root, err := normalizePlanNodeProduct(*product)
	if err != nil {
		return planRephaseError(*asJSON, "INVALID_PRODUCT", err, 2)
	}
	requestAbs, requestRaw, err := readPlanRephaseEvidence(*manifest)
	if err != nil {
		return planRephaseError(*asJSON, "INVALID_MANIFEST", err, 2)
	}
	if learnSHA(requestRaw) != strings.TrimSpace(*manifestSHA) {
		return planRephaseReject(*asJSON, "MANIFEST_SHA_MISMATCH", 3)
	}
	var req planRephaseRequest
	if err := decodePlanRephaseJSON(requestRaw, &req); err != nil {
		return planRephaseError(*asJSON, "INVALID_MANIFEST_JSON", err, 2)
	}
	if err := validatePlanRephaseRequest(req, id); err != nil {
		return planRephaseError(*asJSON, "INVALID_MANIFEST_CONTRACT", err, 2)
	}
	authorityAbs, authorityRaw, err := readPlanRephaseEvidence(*authority)
	if err != nil {
		return planRephaseError(*asJSON, "INVALID_AUTHORITY_RECEIPT", err, 2)
	}
	if learnSHA(authorityRaw) != strings.TrimSpace(*authoritySHA) {
		return planRephaseReject(*asJSON, "AUTHORITY_SHA_MISMATCH", 3)
	}
	var auth planRephaseAuthority
	if err := decodePlanRephaseJSON(authorityRaw, &auth); err != nil {
		return planRephaseError(*asJSON, "INVALID_AUTHORITY_JSON", err, 2)
	}
	eventActor, err := resolveMutationActor(*actorKind, *actor, req.Node.ExpectedOwner)
	if err != nil {
		return planRephaseError(*asJSON, "UNTRUSTED_ACTOR", err, 2)
	}
	if verifyAuthority == nil {
		return planRephaseReject(*asJSON, "TRUSTED_AUTHORITY_VERIFIER_MISSING", 3)
	}
	if err := verifyAuthority(root, req, auth, authorityRaw, eventActor, learnSHA(requestRaw)); err != nil {
		return planRephaseError(*asJSON, "AUTHORITY_NOT_ACCEPTED", err, 3)
	}

	lock, ok := acquireLock(lockName("plan-nodes", root))
	if !ok {
		return planRephaseReject(*asJSON, "PLAN_MUTATION_LOCKED", 1)
	}
	defer lock.release()

	// A prepared crash must recover from immutable local snapshots even if
	// the remote changed or is temporarily unavailable. Fresh Git/provenance
	// CAS is intentionally checked AFTER journal replay/recovery.
	nodePath, err := planNodePathByID(root, id)
	if err != nil {
		return planRephaseError(*asJSON, "NODE_NOT_FOUND", err, 3)
	}
	if filepath.Base(nodePath) != id+".md" {
		return planRephaseReject(*asJSON, "AMBIGUOUS_NODE_ID", 3)
	}
	planPath := filepath.Join(root, "PLAN.md")
	releasePath := filepath.Join(root, "plan", "release-slices.json")
	if err := verifyPlanRephaseControlledPaths(root, nodePath, planPath, releasePath); err != nil {
		return planRephaseError(*asJSON, "CONTROLLED_PATH_LINK_HOLD", err, 3)
	}
	ioLayer := defaultPlanRephaseIO()

	settings, shared, err := readSharedLearningSettings(root)
	if err != nil {
		return planRephaseError(*asJSON, "LEARNING_SETTINGS_INVALID", err, 2)
	}
	journalRoot := filepath.Join(root, ".air-worker", "plan-rephase")
	if shared {
		journalRoot = filepath.Join(settings.RuntimeRoot, "plan-rephase")
	}
	txDir := filepath.Join(journalRoot, learnSHA([]byte(strings.TrimSpace(*requestID))))
	intentPath := filepath.Join(txDir, "intent.json")
	receiptPath := filepath.Join(txDir, "receipt.json")
	snapshotPath := filepath.Join(txDir, "snapshot.json")
	if err := verifyPlanRephaseJournalPaths(journalRoot, txDir, intentPath, receiptPath, snapshotPath,
		filepath.Join(txDir, "node.before"), filepath.Join(txDir, "PLAN.before"), filepath.Join(txDir, "release-slices.before")); err != nil {
		return planRephaseError(*asJSON, "JOURNAL_LINK_HOLD", err, 3)
	}
	if priorRaw, readErr := os.ReadFile(intentPath); readErr == nil {
		var prior planRephaseIntent
		if json.Unmarshal(priorRaw, &prior) != nil || !samePlanRephaseIdentity(prior, strings.TrimSpace(*requestID), learnSHA(requestRaw), learnSHA(authorityRaw), eventActor, req) {
			return planRephaseReject(*asJSON, "REQUEST_ID_REBOUND", 3)
		}
		if prior.Phase != "complete" {
			if prior.Phase == "prepared" {
				if err := reconcilePreparedPlanRephase(ioLayer, txDir, snapshotPath, intentPath, prior,
					nodePath, planPath, releasePath); err != nil {
					return planRephaseError(*asJSON, "PREPARED_RECONCILE_HOLD", err, 3)
				}
				return planRephaseReject(*asJSON, "INTERRUPTED_PREPARED_FILES_RESTORED_NO_BLIND_RETRY", 3)
			}
			return planRephaseReject(*asJSON, "INCOMPLETE_REPHASE_JOURNAL", 3)
		}
		if err := verifyPlanRephaseAfter(ioLayer, nodePath, planPath, releasePath, prior); err != nil {
			return planRephaseError(*asJSON, "COMPLETED_RECEIPT_STATE_MISMATCH", err, 3)
		}
		if err := verifyPlanRephaseCompletedReceipt(receiptPath, prior, req); err != nil {
			return planRephaseError(*asJSON, "COMPLETED_RECEIPT_INVALID", err, 3)
		}
		afterNode, err := readPlanNode(nodePath)
		if err != nil {
			return planRephaseError(*asJSON, "COMPLETED_NODE_INVALID", err, 3)
		}
		eventAt, err := time.Parse(time.RFC3339Nano, prior.ChangedAt)
		if err != nil {
			return planRephaseError(*asJSON, "COMPLETED_EVENT_TIME_INVALID", err, 3)
		}
		evidence := requestAbs + "#sha256=" + learnSHA(requestRaw) + ";authority=" + authorityAbs + "#sha256=" + learnSHA(authorityRaw)
		expectedEvent := planNodeEventRowWithActor(afterNode, "rephased", evidence, "plan-rephase", eventAt, eventActor)
		if err := verifyPlanRephaseNativeEvent(root, settings, shared, expectedEvent); err != nil {
			return planRephaseError(*asJSON, "COMPLETED_EVENT_READBACK_HOLD", err, 3)
		}
		if *asJSON {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"schema": planRephaseReceiptSchema, "status": "PASS", "action": "already_rephased", "request_id": prior.RequestID, "receipt": receiptPath})
		}
		return 0
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return planRephaseError(*asJSON, "JOURNAL_READ_FAILED", readErr, 1)
	}
	if err := verifyPlanRephaseGitCAS(root, req); err != nil {
		return planRephaseError(*asJSON, "GIT_CAS_HOLD", err, 3)
	}
	if err := verifyPlanRephaseProvenance(root, req); err != nil {
		return planRephaseError(*asJSON, "PROVENANCE_CAS_HOLD", err, 3)
	}
	nodeBefore, err := ioLayer.read(nodePath)
	if err != nil {
		return planRephaseError(*asJSON, "READ_NODE_FAILED", err, 1)
	}
	planBefore, err := ioLayer.read(planPath)
	if err != nil {
		return planRephaseError(*asJSON, "READ_PLAN_FAILED", err, 1)
	}
	releaseBefore, err := ioLayer.read(releasePath)
	if err != nil {
		return planRephaseError(*asJSON, "READ_RELEASE_FAILED", err, 1)
	}
	if learnSHA(nodeBefore) != req.Node.ExpectedSHA256 || learnSHA(planBefore) != req.Plan.ExpectedSHA256 || learnSHA(releaseBefore) != req.Release.ExpectedSHA256 {
		return planRephaseReject(*asJSON, "DOCUMENT_CAS_STALE", 3)
	}

	node, err := parsePlanNode(nodePath, nodeBefore)
	if err != nil {
		return planRephaseError(*asJSON, "NODE_INVALID", err, 2)
	}
	if err := validatePlanRephaseNodeBefore(node, req.Node); err != nil {
		return planRephaseError(*asJSON, "NODE_CAS_HOLD", err, 3)
	}
	if req.Node.AfterParent != req.Node.BeforeParent {
		if err := validateExistingPlanParent(root, req.Node.AfterParent, node.ID); err != nil {
			return planRephaseError(*asJSON, "REPARENT_TARGET_INVALID", err, 3)
		}
	}
	afterNode := node
	afterNode.Parent = req.Node.AfterParent
	afterNode.ReturnTo = req.Node.AfterReturnTo
	afterNode.Trigger = req.Node.AfterTrigger
	afterNode.DoneWhen = req.Node.AfterDoneWhen
	changedAt := time.Now().UTC()
	if err := validatePlanNode(afterNode); err != nil {
		return planRephaseError(*asJSON, "NODE_AFTER_INVALID", err, 2)
	}
	nodeAfter, err := plannedRephaseNode(nodeBefore, node, afterNode)
	if err != nil {
		return planRephaseError(*asJSON, "NODE_PATCH_INVALID", err, 3)
	}
	planAfter, err := plannedRephasePlan(planBefore, node, afterNode, req.Plan)
	if err != nil {
		return planRephaseError(*asJSON, "PLAN_REPHASE_INVALID", err, 3)
	}
	releaseAfter, err := plannedRephaseRelease(root, releaseBefore, req.Release)
	if err != nil {
		return planRephaseError(*asJSON, "RELEASE_REPHASE_INVALID", err, 3)
	}

	if shared {
		if err := sharedPlanPreflight(root, settings); err != nil {
			return planRephaseError(*asJSON, "SHARED_PLAN_PREFLIGHT_HOLD", err, 3)
		}
	}
	snapshot := planRephaseSnapshot{
		Schema: "air-worker.plan-rephase-snapshot/v1", RequestID: strings.TrimSpace(*requestID),
		NodeFile: "node.before", PlanFile: "PLAN.before", ReleaseFile: "release-slices.before",
		NodeSHA256: learnSHA(nodeBefore), PlanSHA256: learnSHA(planBefore), ReleaseSHA256: learnSHA(releaseBefore),
	}
	snapshotRaw, _ := json.MarshalIndent(snapshot, "", "  ")
	snapshotRaw = append(snapshotRaw, byte(10))
	snapshotSHA := learnSHA(snapshotRaw)

	intent := planRephaseIntent{
		Schema: planRephaseIntentSchema, RequestID: strings.TrimSpace(*requestID),
		RequestSHA256: learnSHA(requestRaw), AuthoritySHA256: learnSHA(authorityRaw),
		Actor: eventActor, NodeID: id, Repository: req.Repository, CanonicalRef: req.CanonicalRef,
		ExpectedHead: req.ExpectedHead, Generation: req.Generation,
		BeforeNodeSHA256: learnSHA(nodeBefore), AfterNodeSHA256: learnSHA(nodeAfter),
		BeforePlanSHA256: learnSHA(planBefore), AfterPlanSHA256: learnSHA(planAfter),
		BeforeReleaseSHA256: learnSHA(releaseBefore), AfterReleaseSHA256: learnSHA(releaseAfter),
		SnapshotSHA256: snapshotSHA, ChangedAt: changedAt.Format(time.RFC3339Nano), Phase: "prepared",
	}
	if err := os.MkdirAll(txDir, 0o700); err != nil {
		return planRephaseError(*asJSON, "JOURNAL_CREATE_FAILED", err, 1)
	}
	if err := verifyPlanRephaseJournalPaths(journalRoot, txDir, intentPath, receiptPath, snapshotPath,
		filepath.Join(txDir, "node.before"), filepath.Join(txDir, "PLAN.before"), filepath.Join(txDir, "release-slices.before")); err != nil {
		return planRephaseError(*asJSON, "JOURNAL_LINK_AFTER_CREATE_HOLD", err, 3)
	}
	if err := persistPlanRephaseSnapshot(txDir, snapshotPath, snapshotRaw, nodeBefore, planBefore, releaseBefore); err != nil {
		return planRephaseError(*asJSON, "SNAPSHOT_FAILED", err, 1)
	}
	if err := persistPlanRephaseIntent(intentPath, intent); err != nil {
		return planRephaseError(*asJSON, "JOURNAL_PREPARE_FAILED", err, 1)
	}

	files := []planRephaseFile{
		{Path: nodePath, Before: nodeBefore, After: nodeAfter},
		{Path: planPath, Before: planBefore, After: planAfter},
		{Path: releasePath, Before: releaseBefore, After: releaseAfter},
	}
	if err := publishPlanRephaseFiles(ioLayer, files); err != nil {
		return planRephaseError(*asJSON, "PUBLISH_FAILED", err, 1)
	}
	intent.Phase = "event_pending"
	if err := persistPlanRephaseIntent(intentPath, intent); err != nil {
		if rollbackErr := rollbackPlanRephaseFiles(ioLayer, files); rollbackErr != nil {
			return planRephaseError(*asJSON, "JOURNAL_AND_ROLLBACK_UNVERIFIED", fmt.Errorf("%w; rollback: %v", err, rollbackErr), 1)
		}
		return planRephaseError(*asJSON, "JOURNAL_AFTER_PUBLISH_FAILED_FILES_RESTORED", err, 1)
	}

	evidence := requestAbs + "#sha256=" + learnSHA(requestRaw) + ";authority=" + authorityAbs + "#sha256=" + learnSHA(authorityRaw)
	event := planNodeEventRowWithActor(afterNode, "rephased", evidence, "plan-rephase", changedAt, eventActor)
	if err := ioLayer.event(root, []map[string]any{event}); err != nil {
		rollbackErr := rollbackPlanRephaseFiles(ioLayer, files)
		// An event append can partially succeed before reporting failure.
		// Keep the request permanently HOLD for a separate reconcile; never retry blindly.
		intent.Phase = "event_outcome_unknown"
		journalErr := persistPlanRephaseIntent(intentPath, intent)
		if rollbackErr != nil || journalErr != nil {
			return planRephaseError(*asJSON, "EVENT_OUTCOME_AND_ROLLBACK_UNVERIFIED", fmt.Errorf("event: %w; rollback: %v; journal: %v", err, rollbackErr, journalErr), 1)
		}
		return planRephaseError(*asJSON, "EVENT_OUTCOME_UNKNOWN_FILES_RESTORED", err, 1)
	}
	if err := verifyPlanRephaseNativeEvent(root, settings, shared, event); err != nil {
		rollbackErr := rollbackPlanRephaseFiles(ioLayer, files)
		intent.Phase = "event_outcome_unknown"
		journalErr := persistPlanRephaseIntent(intentPath, intent)
		if rollbackErr != nil || journalErr != nil {
			return planRephaseError(*asJSON, "EVENT_READBACK_AND_ROLLBACK_UNVERIFIED", fmt.Errorf("event readback: %w; rollback: %v; journal: %v", err, rollbackErr, journalErr), 1)
		}
		return planRephaseError(*asJSON, "EVENT_READBACK_HOLD_FILES_RESTORED", err, 1)
	}
	intent.Phase = "event_written"
	if err := persistPlanRephaseIntent(intentPath, intent); err != nil {
		return planRephaseError(*asJSON, "EVENT_WRITTEN_JOURNAL_FAILED", err, 1)
	}

	receipt := planRephaseReceipt{
		Schema: planRephaseReceiptSchema, Status: "PASS", RequestID: intent.RequestID,
		RequestSHA256: intent.RequestSHA256, AuthorityReceiptSHA256: intent.AuthoritySHA256,
		Actor: eventActor, Repository: req.Repository, CanonicalRef: req.CanonicalRef,
		ObservedHead: req.ExpectedHead, ObservedRemoteHead: req.ExpectedHead,
		AcceptedAncestor: req.AcceptedAncestor, Generation: req.Generation,
		HistoryManifestSHA256: req.HistoryManifestSHA256, HistoryManifestPath: req.HistoryManifestPath,
		SourceProvenanceRef: req.SourceProvenanceRef, SourceProvenanceCommit: req.SourceProvenanceCommit,
		NodeID: id, StatusPreserved: true, ReceiptsPreserved: true, SilentClosure: false,
		ImportedHistoricalNodes: 0, BeforeNodeSHA256: intent.BeforeNodeSHA256, AfterNodeSHA256: intent.AfterNodeSHA256,
		BeforePlanSHA256: intent.BeforePlanSHA256, AfterPlanSHA256: intent.AfterPlanSHA256,
		BeforeHeaderSHA256: req.Plan.HeaderBeforeSHA256, AfterHeaderSHA256: learnSHA([]byte(req.Plan.HeaderAfter)),
		BeforeReleaseSHA256: intent.BeforeReleaseSHA256, AfterReleaseSHA256: intent.AfterReleaseSHA256,
		BeforeParent: req.Node.BeforeParent, AfterParent: req.Node.AfterParent,
		BeforeTrigger: req.Node.BeforeTrigger, AfterTrigger: req.Node.AfterTrigger,
		BeforeDoneWhen: req.Node.BeforeDoneWhen, AfterDoneWhen: req.Node.AfterDoneWhen,
		ReleaseVersion: req.Release.Version, ReleaseStepID: req.Release.StepID,
		BeforeNodeRefs: req.Release.BeforeNodeRefs, AfterNodeRefs: req.Release.AfterNodeRefs,
		RollbackSnapshot: snapshotPath, RollbackSnapshotSHA256: snapshotSHA,
		ReadbackChecks:  []string{"node_sha", "plan_sha", "release_slices_sha", "native_event"},
		NoReleaseEffect: true, NoInstallEffect: true, NoProviderEffect: true,
		ChangedAt: changedAt.Format(time.RFC3339Nano),
	}
	if err := persistPlanRephaseReceipt(receiptPath, receipt); err != nil {
		// The native event may be durable even when a receipt fails. Never claim
		// success or silently replay; restore bounded files only when CAS permits.
		rollbackErr := rollbackPlanRephaseFiles(ioLayer, files)
		intent.Phase = "receipt_failed_event_outcome_unknown"
		journalErr := persistPlanRephaseIntent(intentPath, intent)
		if rollbackErr != nil || journalErr != nil {
			return planRephaseError(*asJSON, "RECEIPT_AND_ROLLBACK_UNVERIFIED", fmt.Errorf("receipt: %w; rollback: %v; journal: %v", err, rollbackErr, journalErr), 1)
		}
		return planRephaseError(*asJSON, "RECEIPT_FAILED_EVENT_UNKNOWN_FILES_RESTORED", err, 1)
	}
	receiptEncoded, _ := json.MarshalIndent(receipt, "", "  ")
	intent.ReceiptSHA256 = learnSHA(append(receiptEncoded, byte(10)))
	intent.Phase = "complete"
	if err := persistPlanRephaseIntent(intentPath, intent); err != nil {
		return planRephaseError(*asJSON, "FINAL_JOURNAL_FAILED", err, 1)
	}
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"schema": planRephaseReceiptSchema, "status": "PASS", "action": "rephased",
			"node_id": id, "request_id": intent.RequestID, "receipt": receiptPath,
			"node_sha256": intent.AfterNodeSHA256, "plan_sha256": intent.AfterPlanSHA256,
			"release_slices_sha256": intent.AfterReleaseSHA256,
		})
	}
	return 0
}

func readPlanRephaseEvidence(path string) (string, []byte, error) {
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", nil, err
	}
	if err := checkPlanRephaseNoLinkedAncestors(abs); err != nil {
		return "", nil, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() < 2 || info.Size() > planRephaseMaxEvidence {
		return "", nil, fmt.Errorf("evidence must be a regular non-symlink file within 2..%d bytes", planRephaseMaxEvidence)
	}
	raw, err := os.ReadFile(abs)
	return abs, raw, err
}

func decodePlanRephaseJSON(raw []byte, out any) error {
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	return nil
}

func validatePlanRephaseRequest(req planRephaseRequest, id string) error {
	if req.Schema != planRephaseRequestSchema {
		return fmt.Errorf("schema must be %s", planRephaseRequestSchema)
	}
	if req.Node.ID != id || !planNodeIDRE.MatchString(req.Node.ID) {
		return fmt.Errorf("request node id mismatch")
	}
	if strings.TrimSpace(req.Repository) == "" || strings.TrimSpace(req.RemoteName) == "" {
		return fmt.Errorf("repository and remote_name are required")
	}
	if !planRephaseRef.MatchString(req.CanonicalRef) || strings.Contains(req.CanonicalRef, "..") || strings.HasSuffix(req.CanonicalRef, "/") {
		return fmt.Errorf("invalid canonical ref")
	}
	for name, value := range map[string]string{
		"expected_head": req.ExpectedHead, "accepted_ancestor": req.AcceptedAncestor,
		"source_provenance_commit": req.SourceProvenanceCommit,
	} {
		if !planRephaseSHA40.MatchString(value) {
			return fmt.Errorf("%s must be exact lowercase 40-hex commit", name)
		}
	}
	for name, value := range map[string]string{
		"history_manifest_sha256":   req.HistoryManifestSHA256,
		"node.expected_sha256":      req.Node.ExpectedSHA256,
		"plan.expected_sha256":      req.Plan.ExpectedSHA256,
		"plan.header_before_sha256": req.Plan.HeaderBeforeSHA256,
		"release.expected_sha256":   req.Release.ExpectedSHA256,
	} {
		if !planRephaseSHA256.MatchString(value) {
			return fmt.Errorf("%s must be lowercase SHA256", name)
		}
	}
	if strings.TrimSpace(req.Generation) == "" || len(req.Generation) > 128 ||
		strings.TrimSpace(req.HistoryManifestPath) == "" || len(req.HistoryManifestPath) > 1024 ||
		!planRephaseRef.MatchString(req.SourceProvenanceRef) || strings.Contains(req.SourceProvenanceRef, "..") {
		return fmt.Errorf("generation, history manifest path and exact source provenance ref are required")
	}
	for name, value := range map[string]string{
		"expected_owner": req.Node.ExpectedOwner,
		"before_parent":  req.Node.BeforeParent, "after_parent": req.Node.AfterParent,
		"before_return_to": req.Node.BeforeReturnTo, "after_return_to": req.Node.AfterReturnTo,
		"before_trigger": req.Node.BeforeTrigger, "after_trigger": req.Node.AfterTrigger,
		"before_done_when": req.Node.BeforeDoneWhen, "after_done_when": req.Node.AfterDoneWhen,
		"header_before": req.Plan.HeaderBefore, "header_after": req.Plan.HeaderAfter,
		"release.version": req.Release.Version, "release.step_id": req.Release.StepID,
		"release.before_trigger": req.Release.BeforeTrigger, "release.after_trigger": req.Release.AfterTrigger,
		"release.before_done_when": req.Release.BeforeDoneWhen, "release.after_done_when": req.Release.AfterDoneWhen,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	if len(req.Plan.HeaderBefore) > 64*1024 || len(req.Plan.HeaderAfter) > 64*1024 || req.Plan.HeaderBefore == req.Plan.HeaderAfter {
		return fmt.Errorf("header rephase must be one bounded real change")
	}
	if learnSHA([]byte(req.Plan.HeaderBefore)) != req.Plan.HeaderBeforeSHA256 {
		return fmt.Errorf("header before sha mismatch")
	}
	// One-node scoped rebind is an all-or-nothing request, never an implicit
	// side-effect of changing trigger/done_when. The same pair must be checked
	// and applied at the active release AND selected step.
	refs := req.Release
	if len(refs.BeforeNodeRefs) != 0 || len(refs.AfterNodeRefs) != 0 {
		if len(refs.BeforeNodeRefs) != 1 || len(refs.AfterNodeRefs) != 1 {
			return fmt.Errorf("scoped rebind requires one old and one new full node reference")
		}
		oldID, newID := refs.BeforeNodeRefs[0], refs.AfterNodeRefs[0]
		if len(oldID) > 128 || !regexp.MustCompile(`^N-[0-9]{3}(?:_[A-Za-z0-9-]+)?$`).MatchString(oldID) ||
			!planNodeIDRE.MatchString(newID) || newID != req.Node.ID || oldID == newID {
			return fmt.Errorf("scoped rebind identity must target exactly the amended OPEN full node")
		}
		if req.Node.BeforeParent != req.Node.AfterParent ||
			req.Node.BeforeReturnTo != req.Node.AfterReturnTo ||
			!strings.Contains(req.Node.AfterTrigger, req.Node.BeforeTrigger) ||
			!strings.Contains(req.Node.AfterDoneWhen, req.Node.BeforeDoneWhen) {
			return fmt.Errorf("scoped rebind may not remove prior node trigger/obligations or move its M3 authority")
		}
	}
	return nil
}

func validatePlanRephaseAuthority(auth planRephaseAuthority, req planRephaseRequest, actor, requestSHA string) error {
	if auth.Schema != planRephaseAuthoritySchema || auth.Operation != "plan.rephase.apply" || !auth.Approved {
		return fmt.Errorf("authority receipt does not approve plan.rephase.apply")
	}
	if auth.Repository != req.Repository || auth.RemoteName != req.RemoteName || auth.CanonicalRef != req.CanonicalRef ||
		auth.ExpectedHead != req.ExpectedHead || auth.Generation != req.Generation || auth.Actor != actor ||
		auth.NodeID != req.Node.ID || auth.ReleaseVersion != req.Release.Version || auth.RequestSHA256 != requestSHA {
		return fmt.Errorf("authority receipt does not exactly bind request actor/ref/generation")
	}
	return nil
}

func verifyPlanRephaseGitCAS(root string, req planRephaseRequest) error {
	remoteURL, err := planRephaseGit(root, "remote", "get-url", req.RemoteName)
	if err != nil || strings.TrimSpace(remoteURL) != req.Repository {
		return fmt.Errorf("repository/remote mismatch")
	}
	ref, err := planRephaseGit(root, "symbolic-ref", "-q", "HEAD")
	if err != nil || strings.TrimSpace(ref) != req.CanonicalRef {
		return fmt.Errorf("canonical ref mismatch")
	}
	head, err := planRephaseGit(root, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(head) != req.ExpectedHead {
		return fmt.Errorf("stale expected HEAD")
	}
	cmd := exec.Command("git", "-C", root, "merge-base", "--is-ancestor", req.AcceptedAncestor, req.ExpectedHead)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("accepted ancestry mismatch: %s", strings.TrimSpace(string(out)))
	}
	remote, err := planRephaseGit(root, "ls-remote", req.RemoteName, req.CanonicalRef)
	if err != nil {
		return fmt.Errorf("remote ref readback failed")
	}
	fields := strings.Fields(remote)
	if len(fields) != 2 || fields[0] != req.ExpectedHead || fields[1] != req.CanonicalRef {
		return fmt.Errorf("remote ref/head mismatch")
	}
	return nil
}

func planRephaseGit(root string, args ...string) (string, error) {
	cmdArgs := append([]string{"-C", root}, args...)
	cmd := exec.Command("git", cmdArgs...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// verifyPlanRephaseControlledPaths rejects linked/junction ancestors and target files.
// A bounded node/PLAN/release mutation may never escape the selected product.
func verifyPlanRephaseControlledPaths(root string, paths ...string) error {
	product, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	for _, p := range append([]string{product}, paths...) {
		abs, err := filepath.Abs(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(product, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("controlled path outside product: %s", p)
		}
		if err := checkPlanRephaseNoLinkedAncestors(abs); err != nil {
			return err
		}
	}
	return nil
}

// checkPlanRephaseNoLinkedAncestors detects both file links and linked
// intermediate directories (including Windows junctions exposed by Lstat).
func checkPlanRephaseNoLinkedAncestors(path string) error {
	for current := filepath.Clean(path); ; {
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("path lstat %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("linked ancestor or target: %s", current)
		}
		next := filepath.Dir(current)
		if next == current {
			break
		}
		current = next
	}
	return nil
}

// verifyPlanRephaseJournalPaths treats the dedicated durable journal as a
// controlled write target. A shared-runtime journal is allowed only under the
// explicit shared settings root selected by the caller, never via a link.
func verifyPlanRephaseJournalPaths(journalRoot, txDir string, paths ...string) error {
	base, err := filepath.Abs(journalRoot)
	if err != nil {
		return err
	}
	child, err := filepath.Abs(txDir)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(base, child)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("transaction path outside selected journal root")
	}
	for _, path := range append([]string{base, child}, paths...) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(child, abs)
		if err != nil {
			return err
		}
		if path != journalRoot && path != txDir &&
			(rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator))) {
			return fmt.Errorf("journal entry escapes transaction directory")
		}
		for current := abs; ; {
			info, err := os.Lstat(current)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("journal ancestor inspect %s: %w", current, err)
			}
			if err == nil {
				if info.Mode()&os.ModeSymlink != 0 {
					return fmt.Errorf("linked journal ancestor: %s", current)
				}
				// Windows junctions can expose a link target despite a regular
				// mode; Readlink returning a target is always a hard HOLD.
				if dest, readlinkErr := os.Readlink(current); readlinkErr == nil {
					return fmt.Errorf("journal reparse ancestor: %s -> %s", current, dest)
				}
			}
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}
	return nil
}

// verifyPlanRephaseNativeEvent reads back the exact persisted native event
// after the event writer returns. The bounded journal tail prevents whole-vault
// scans; missing or ambiguous records are HOLD, never a claimed native PASS.
func verifyPlanRephaseNativeEvent(root string, settings sharedLearningSettings, shared bool, expected map[string]any) error {
	id, _ := expected["id"].(string)
	observed, _ := expected["observed"].(string)
	evidence, _ := expected["evidence"].(string)
	actor, _ := expected["actor"].(string)
	reference, _ := expected["reference"].(string)
	if id == "" || observed == "" || actor == "" || reference == "" {
		return fmt.Errorf("native event identity incomplete")
	}
	path := planNodeEventsPath(root)
	if shared {
		path = filepath.Join(settings.RuntimeRoot, "events.jsonl")
	}
	if err := checkPlanRephaseNoLinkedAncestors(path); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("native event store not regular: %v", err)
	}
	const tailMax int64 = 4 * 1024 * 1024
	length := info.Size()
	if length == 0 {
		return fmt.Errorf("native event journal is empty")
	}
	if length > tailMax {
		length = tailMax
	}
	start := info.Size() - length
	raw, err := io.ReadAll(io.NewSectionReader(f, start, length))
	if err != nil {
		return err
	}
	if start > 0 {
		firstNewline := bytes.IndexByte(raw, '\n')
		if firstNewline < 0 {
			return fmt.Errorf("native event tail contains no complete records")
		}
		raw = raw[firstNewline+1:]
	}
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		return fmt.Errorf("native event journal has incomplete terminal record")
	}
	found := 0
	for _, line := range bytes.Split(raw, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal(line, &row); err != nil {
			return fmt.Errorf("native event tail contains invalid JSON: %w", err)
		}
		expectedAt, ok := expected["created_at"].(string)
		if !ok || expectedAt == "" {
			return fmt.Errorf("expected native event timestamp missing")
		}
		startedAt, err := time.Parse(time.RFC3339Nano, expectedAt)
		if err != nil {
			return fmt.Errorf("expected native event timestamp invalid: %w", err)
		}
		if shared {
			if row["run_id"] != id {
				continue
			}
			persistedID, idOK := row["event_id"].(string)
			persistedAt, atOK := row["at"].(string)
			eventAt, atErr := time.Parse(time.RFC3339Nano, persistedAt)
			if row["schema"] == "air.learning.event/v1" && row["observed"] == observed &&
				row["class"] == "plan-node" && row["source"] == "plan-node" &&
				row["principal"] == actor && row["session"] == actor &&
				row["outcome_ref"] == evidence && row["kind"] == "observation" &&
				row["product_id"] == settings.ProductID && settings.ProductID != "" &&
				idOK && planRephaseEventID.MatchString(persistedID) &&
				atOK && atErr == nil && !eventAt.Before(startedAt) &&
				!eventAt.After(startedAt.Add(5*time.Minute)) {
				found++
			} else {
				return fmt.Errorf("shared native event identity, product or timestamp mismatch")
			}
		} else {
			if row["id"] != id {
				continue
			}
			if row["schema"] == learnSchemaVersion && row["observed"] == observed &&
				row["class"] == "plan-node" && row["source"] == "plan-rephase" &&
				row["actor"] == actor && row["reference"] == reference &&
				row["evidence"] == evidence && row["kind"] == "check" &&
				row["created_at"] == expectedAt {
				found++
			} else {
				return fmt.Errorf("native event identity or timestamp mismatch")
			}
		}
	}
	if found != 1 {
		return fmt.Errorf("expected exactly one bound native event, found %d", found)
	}
	return nil
}

func verifyPlanRephaseProvenance(root string, req planRephaseRequest) error {
	_, raw, err := readPlanRephaseEvidence(req.HistoryManifestPath)
	if err != nil {
		return fmt.Errorf("history manifest unreadable: %w", err)
	}
	if learnSHA(raw) != req.HistoryManifestSHA256 {
		return fmt.Errorf("history manifest exact bytes changed")
	}
	var h planRephaseHistoryEvidence
	if err := decodePlanRephaseJSON(raw, &h); err != nil {
		return fmt.Errorf("history manifest invalid: %w", err)
	}
	if h.Schema != planRephaseHistorySchema || h.Repository != req.Repository ||
		h.CanonicalRef != req.CanonicalRef || h.ExpectedHead != req.ExpectedHead ||
		h.Generation != req.Generation || h.SourceProvenanceRef != req.SourceProvenanceRef ||
		h.SourceProvenanceCommit != req.SourceProvenanceCommit {
		return fmt.Errorf("history manifest identity/generation/source mismatch")
	}
	commit, err := planRephaseGit(root, "rev-parse", "--verify", req.SourceProvenanceRef+"^{commit}")
	if err != nil || commit != req.SourceProvenanceCommit {
		return fmt.Errorf("source provenance local ref/commit mismatch")
	}
	remote, err := planRephaseGit(root, "ls-remote", req.RemoteName, req.SourceProvenanceRef)
	if err != nil {
		return fmt.Errorf("source provenance remote readback failed: %w", err)
	}
	fields := strings.Fields(remote)
	if len(fields) != 2 || fields[0] != req.SourceProvenanceCommit || fields[1] != req.SourceProvenanceRef {
		return fmt.Errorf("source provenance remote ref/commit mismatch")
	}
	return nil
}

func validatePlanRephaseNodeBefore(node planNode, want planRephaseNode) error {
	if node.ID != want.ID || node.Status != "open" || node.Owner != want.ExpectedOwner ||
		node.Parent != want.BeforeParent || node.ReturnTo != want.BeforeReturnTo ||
		node.Trigger != want.BeforeTrigger || node.DoneWhen != want.BeforeDoneWhen {
		return fmt.Errorf("node identity/status/owner/original fields differ from expected CAS")
	}
	return nil
}

// plannedRephaseNode changes only the four allowed frontmatter scalars.
// It deliberately preserves all other bytes, including unknown metadata and receipts.
func plannedRephaseNode(before []byte, oldNode, newNode planNode) ([]byte, error) {
	if !bytes.HasPrefix(before, []byte("---\n")) {
		return nil, fmt.Errorf("node must start with canonical frontmatter")
	}
	fields := []struct {
		key, was, now string
		count         int
	}{
		{key: "parent", was: oldNode.Parent, now: newNode.Parent},
		{key: "return_to", was: oldNode.ReturnTo, now: newNode.ReturnTo},
		{key: "trigger", was: oldNode.Trigger, now: newNode.Trigger},
		{key: "done_when", was: oldNode.DoneWhen, now: newNode.DoneWhen},
	}
	lines := bytes.SplitAfter(before, []byte("\n"))
	closed, changed := false, false
	for i := 1; i < len(lines); i++ {
		raw := string(lines[i])
		value := strings.TrimSuffix(strings.TrimSuffix(raw, "\n"), "\r")
		if value == "---" {
			closed = true
			break
		}
		for j := range fields {
			field := &fields[j]
			if !strings.HasPrefix(value, field.key+":") {
				continue
			}
			field.count++
			expected := field.key + ": " + yamlScalar(field.was)
			if value != expected {
				return nil, fmt.Errorf("noncanonical or stale node field %s", field.key)
			}
			if field.was != field.now {
				ending := raw[len(value):]
				lines[i] = []byte(field.key + ": " + yamlScalar(field.now) + ending)
				changed = true
			}
		}
	}
	if !closed || !changed {
		return nil, fmt.Errorf("node header missing or no real bounded field change")
	}
	for _, field := range fields {
		if field.count != 1 {
			return nil, fmt.Errorf("node field %s must occur exactly once", field.key)
		}
	}
	out := bytes.Join(lines, nil)
	return out, nil
}

func plannedRephasePlan(before []byte, oldNode, newNode planNode, p planRephasePlan) ([]byte, error) {
	if learnSHA(before) != p.ExpectedSHA256 {
		return nil, fmt.Errorf("PLAN SHA changed")
	}
	if bytes.Count(before, []byte(p.HeaderBefore)) != 1 {
		return nil, fmt.Errorf("PLAN header span is absent or ambiguous")
	}
	oldLine := []byte(spineNodeLine(oldNode))
	newLine := []byte(spineNodeLine(newNode))
	if bytes.Contains([]byte(p.HeaderBefore), oldLine) {
		return nil, fmt.Errorf("header span overlaps native node spine")
	}
	out := append([]byte(nil), before...)
	if bytes.Count(out, oldLine) != 1 {
		return nil, fmt.Errorf("PLAN spine has no unique exact node link")
	}
	if !bytes.Equal(oldLine, newLine) {
		out = bytes.Replace(out, oldLine, newLine, 1)
	}
	if bytes.Count(out, []byte(p.HeaderBefore)) != 1 {
		return nil, fmt.Errorf("PLAN header span changed during spine rephase")
	}
	out = bytes.Replace(out, []byte(p.HeaderBefore), []byte(p.HeaderAfter), 1)
	return out, nil
}

func plannedRephaseRelease(_ string, before []byte, r planRephaseRelease) ([]byte, error) {
	if learnSHA(before) != r.ExpectedSHA256 {
		return nil, fmt.Errorf("release-slices SHA changed")
	}
	targetRaw, target, err := findPlanRephaseStep(before, r.Version, r.StepID)
	if err != nil {
		return nil, err
	}
	if target.Status != "open" || target.Trigger != r.BeforeTrigger || target.DoneWhen != r.BeforeDoneWhen {
		return nil, fmt.Errorf("release step CAS mismatch or step is not OPEN")
	}
	afterStep, err := replacePlanRephaseJSONField(targetRaw, "trigger", r.BeforeTrigger, r.AfterTrigger)
	if err != nil {
		return nil, err
	}
	afterStep, err = replacePlanRephaseJSONField(afterStep, "done_when", r.BeforeDoneWhen, r.AfterDoneWhen)
	if err != nil {
		return nil, err
	}
	if bytes.Count(before, targetRaw) != 1 {
		return nil, fmt.Errorf("target release step bytes are absent or ambiguous")
	}
	out := bytes.Replace(before, targetRaw, afterStep, 1)
	if len(r.BeforeNodeRefs) == 1 && len(r.AfterNodeRefs) == 1 {
		// The active release and the selected OPEN step MUST both carry the
		// exact prior reference. Patch both inside the same three-file native
		// transaction; no intermediate release/step mixed generation is written.
		afterStep, err = replacePlanRephaseJSONRefArray(afterStep, r.BeforeNodeRefs, r.AfterNodeRefs)
		if err != nil {
			return nil, fmt.Errorf("step node_refs: %w", err)
		}
		releaseRaw, stepsRaw, err := findPlanRephaseReleaseObject(before, r.Version)
		if err != nil {
			return nil, err
		}
		if bytes.Count(before, releaseRaw) != 1 || bytes.Count(stepsRaw, targetRaw) != 1 {
			return nil, fmt.Errorf("release or selected step is ambiguous")
		}
		afterSteps := bytes.Replace(stepsRaw, targetRaw, afterStep, 1)
		// Temporarily mask the ENTIRE steps array so the release-level field
		// cannot be confused with a nested step's identically named field.
		marker := []byte{0, 1, 2, 3}
		if bytes.Contains(releaseRaw, marker) || bytes.Count(releaseRaw, stepsRaw) != 1 {
			return nil, fmt.Errorf("release steps location is ambiguous")
		}
		masked := bytes.Replace(releaseRaw, stepsRaw, marker, 1)
		masked, err = replacePlanRephaseJSONRefArray(masked, r.BeforeNodeRefs, r.AfterNodeRefs)
		if err != nil {
			return nil, fmt.Errorf("release node_refs: %w", err)
		}
		afterRelease := bytes.Replace(masked, marker, afterSteps, 1)
		out = bytes.Replace(before, releaseRaw, afterRelease, 1)
	}
	_, afterView, err := findPlanRephaseStep(out, r.Version, r.StepID)
	if err != nil {
		return nil, fmt.Errorf("after release-slices validation: %w", err)
	}
	if afterView.Status != "open" || afterView.Trigger != r.AfterTrigger || afterView.DoneWhen != r.AfterDoneWhen {
		return nil, fmt.Errorf("after release step does not match requested rephase")
	}
	if len(r.AfterNodeRefs) == 1 {
		releaseAfter, _, err := findPlanRephaseReleaseObject(out, r.Version)
		if err != nil {
			return nil, err
		}
		stepAfter, _, err := findPlanRephaseStep(out, r.Version, r.StepID)
		if err != nil {
			return nil, err
		}
		for _, scope := range [][]byte{releaseAfter, stepAfter} {
			refs, refErr := readPlanRephaseNodeRefs(scope)
			if refErr != nil || len(refs) != 1 || refs[0] != r.AfterNodeRefs[0] {
				return nil, fmt.Errorf("atomic node_refs readback mismatch")
			}
		}
	}
	return out, nil
}

// The scope helpers preserve unrelated JSON bytes, reject ambiguous keys and
// fail closed on absent/stale references. They never create a new node.
func findPlanRephaseReleaseObject(raw []byte, version string) ([]byte, []byte, error) {
	var doc struct {
		Releases []json.RawMessage `json:"releases"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil, err
	}
	var found []byte
	var steps []byte
	for _, release := range doc.Releases {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(release, &fields); err != nil {
			return nil, nil, err
		}
		var v string
		if err := json.Unmarshal(fields["version"], &v); err != nil {
			return nil, nil, err
		}
		if v != version {
			continue
		}
		if found != nil {
			return nil, nil, fmt.Errorf("duplicate release version")
		}
		found = release
		steps = fields["steps"]
	}
	if found == nil || len(steps) == 0 || bytes.Count(found, steps) != 1 {
		return nil, nil, fmt.Errorf("target release steps absent or ambiguous")
	}
	return found, steps, nil
}

func readPlanRephaseNodeRefs(raw []byte) ([]string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	if len(fields["node_refs"]) == 0 {
		return nil, fmt.Errorf("node_refs missing")
	}
	var refs []string
	if err := json.Unmarshal(fields["node_refs"], &refs); err != nil {
		return nil, err
	}
	return refs, nil
}

func replacePlanRephaseJSONRefArray(raw []byte, before, after []string) ([]byte, error) {
	// After the steps array is masked this must match exactly ONE field at the
	// current object scope. Nested/duplicate ambiguities HOLD before writing.
	pattern := regexp.MustCompile(`"node_refs"\s*:\s*(\[[^\[\]]*\])`)
	matches := pattern.FindAllSubmatchIndex(raw, -1)
	if len(matches) != 1 || len(matches[0]) < 4 {
		return nil, fmt.Errorf("node_refs absent or ambiguous")
	}
	m := matches[0]
	var current []string
	if err := json.Unmarshal(raw[m[2]:m[3]], &current); err != nil ||
		len(current) != 1 || len(before) != 1 || len(after) != 1 || current[0] != before[0] {
		return nil, fmt.Errorf("stale node_refs CAS")
	}
	encoded, err := json.Marshal(after)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(raw)+len(encoded)-(m[3]-m[2]))
	out = append(out, raw[:m[2]]...)
	out = append(out, encoded...)
	out = append(out, raw[m[3]:]...)
	return out, nil
}

type planRephaseStepView struct {
	ID       string
	Status   string
	Trigger  string
	DoneWhen string
}

func findPlanRephaseStep(raw []byte, version, stepID string) ([]byte, planRephaseStepView, error) {
	var empty planRephaseStepView
	if len(raw) == 0 || len(raw) > 1<<20 {
		return nil, empty, fmt.Errorf("release-slices must be 1..1MiB")
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return nil, empty, err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, empty, err
	}
	var schema, active string
	if err := json.Unmarshal(top["schema"], &schema); err != nil || schema != releaseSlicesSchema {
		return nil, empty, fmt.Errorf("release-slices schema must be %s", releaseSlicesSchema)
	}
	if err := json.Unmarshal(top["active_release"], &active); err != nil || active != version {
		return nil, empty, fmt.Errorf("requested release is not active")
	}
	var releases []json.RawMessage
	if err := json.Unmarshal(top["releases"], &releases); err != nil {
		return nil, empty, fmt.Errorf("release-slices releases are invalid")
	}
	releaseHits := 0
	stepHits := 0
	var targetRaw []byte
	var target planRephaseStepView
	for _, releaseRaw := range releases {
		var rel map[string]json.RawMessage
		if err := json.Unmarshal(releaseRaw, &rel); err != nil {
			return nil, empty, err
		}
		var relVersion, relStatus string
		_ = json.Unmarshal(rel["version"], &relVersion)
		if relVersion != version {
			continue
		}
		releaseHits++
		if err := json.Unmarshal(rel["status"], &relStatus); err != nil || relStatus != "active" {
			return nil, empty, fmt.Errorf("requested release is not active")
		}
		var steps []json.RawMessage
		if err := json.Unmarshal(rel["steps"], &steps); err != nil {
			return nil, empty, fmt.Errorf("release steps are invalid")
		}
		for _, stepRaw := range steps {
			var stepFields map[string]json.RawMessage
			if err := json.Unmarshal(stepRaw, &stepFields); err != nil {
				return nil, empty, err
			}
			var view planRephaseStepView
			_ = json.Unmarshal(stepFields["id"], &view.ID)
			_ = json.Unmarshal(stepFields["status"], &view.Status)
			_ = json.Unmarshal(stepFields["trigger"], &view.Trigger)
			_ = json.Unmarshal(stepFields["done_when"], &view.DoneWhen)
			if view.ID == stepID {
				stepHits++
				targetRaw = append([]byte(nil), stepRaw...)
				target = view
			}
		}
	}
	if releaseHits != 1 || stepHits != 1 || len(targetRaw) == 0 {
		return nil, empty, fmt.Errorf("release/step is absent or ambiguous")
	}
	return targetRaw, target, nil
}

func replacePlanRephaseJSONField(raw []byte, key, before, after string) ([]byte, error) {
	oldValue, _ := json.Marshal(before)
	newValue, _ := json.Marshal(after)
	keyPattern := regexp.QuoteMeta("\"" + key + "\"")
	valuePattern := regexp.QuoteMeta(string(oldValue))
	pattern := regexp.MustCompile("(" + keyPattern + "\\s*:\\s*)" + valuePattern)
	matches := pattern.FindAllSubmatchIndex(raw, -1)
	if len(matches) != 1 || len(matches[0]) < 4 {
		return nil, fmt.Errorf("release step field %s is absent, ambiguous, or stale", key)
	}
	m := matches[0]
	out := make([]byte, 0, len(raw)-len(oldValue)+len(newValue))
	out = append(out, raw[:m[2]]...)
	out = append(out, raw[m[2]:m[3]]...)
	out = append(out, newValue...)
	out = append(out, raw[m[1]:]...)
	return out, nil
}

type planRephaseFile struct {
	Path   string
	Before []byte
	After  []byte
}

func publishPlanRephaseFiles(ioLayer planRephaseIO, files []planRephaseFile) error {
	for i := range files {
		if err := ioLayer.write(files[i].Path, files[i].After); err != nil {
			// Even the failing durable write may have replaced its target before returning.
			if rollbackErr := rollbackPlanRephaseFiles(ioLayer, files[:i+1]); rollbackErr != nil {
				return fmt.Errorf("publish %s: %w; rollback UNVERIFIED: %v", filepath.Base(files[i].Path), err, rollbackErr)
			}
			return fmt.Errorf("publish %s: %w; rollback verified", filepath.Base(files[i].Path), err)
		}
	}
	for i := range files {
		got, err := ioLayer.read(files[i].Path)
		if err != nil || !bytes.Equal(got, files[i].After) {
			if rollbackErr := rollbackPlanRephaseFiles(ioLayer, files); rollbackErr != nil {
				return fmt.Errorf("readback %s failed; rollback UNVERIFIED: %v", filepath.Base(files[i].Path), rollbackErr)
			}
			if err != nil {
				return fmt.Errorf("readback %s: %w; rollback verified", filepath.Base(files[i].Path), err)
			}
			return fmt.Errorf("readback mismatch for %s; rollback verified", filepath.Base(files[i].Path))
		}
	}
	return nil
}

func rollbackPlanRephaseFiles(ioLayer planRephaseIO, files []planRephaseFile) error {
	// Fail without writing if any controlled file has drifted outside the
	// exact before/after CAS generations (another writer or corrupt data).
	for _, file := range files {
		current, err := ioLayer.read(file.Path)
		if err != nil {
			return fmt.Errorf("rollback preflight %s: %w", filepath.Base(file.Path), err)
		}
		if !bytes.Equal(current, file.Before) && !bytes.Equal(current, file.After) {
			return fmt.Errorf("rollback CAS drift %s", filepath.Base(file.Path))
		}
	}
	var first error
	for i := len(files) - 1; i >= 0; i-- {
		if err := ioLayer.write(files[i].Path, files[i].Before); err != nil && first == nil {
			first = fmt.Errorf("rollback write %s: %w", filepath.Base(files[i].Path), err)
		}
	}
	for _, f := range files {
		raw, err := ioLayer.read(f.Path)
		if err != nil && first == nil {
			first = fmt.Errorf("rollback readback %s: %w", filepath.Base(f.Path), err)
		} else if err == nil && !bytes.Equal(raw, f.Before) && first == nil {
			first = fmt.Errorf("rollback readback mismatch %s", filepath.Base(f.Path))
		}
	}
	return first
}

func persistPlanRephaseSnapshot(txDir, metaPath string, meta, node, plan, release []byte) error {
	for name, raw := range map[string][]byte{
		"node.before": node, "PLAN.before": plan, "release-slices.before": release, filepath.Base(metaPath): meta,
	} {
		path := filepath.Join(txDir, name)
		if existing, err := os.ReadFile(path); err == nil {
			if !bytes.Equal(existing, raw) {
				return fmt.Errorf("rollback snapshot collision: %s", name)
			}
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := writeFileAtomicDurable(path, raw); err != nil {
			return err
		}
	}
	return nil
}

func persistPlanRephaseIntent(path string, intent planRephaseIntent) error {
	raw, err := json.MarshalIndent(intent, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomicDurable(path, append(raw, byte(10)))
}

func persistPlanRephaseReceipt(path string, receipt planRephaseReceipt) error {
	raw, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, byte(10))
	if existing, err := os.ReadFile(path); err == nil {
		if !bytes.Equal(existing, raw) {
			return fmt.Errorf("immutable receipt collision")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeFileAtomicDurable(path, raw)
}

func samePlanRephaseIdentity(prior planRephaseIntent, requestID, requestSHA, authoritySHA, actor string, req planRephaseRequest) bool {
	return prior.Schema == planRephaseIntentSchema && prior.RequestID == requestID &&
		prior.RequestSHA256 == requestSHA && prior.AuthoritySHA256 == authoritySHA &&
		prior.Actor == actor && prior.NodeID == req.Node.ID && prior.Repository == req.Repository &&
		prior.CanonicalRef == req.CanonicalRef && prior.ExpectedHead == req.ExpectedHead &&
		prior.Generation == req.Generation
}

func samePlanRephaseBinding(a, b planRephaseIntent) bool {
	return a.Schema == b.Schema && a.RequestID == b.RequestID && a.RequestSHA256 == b.RequestSHA256 &&
		a.AuthoritySHA256 == b.AuthoritySHA256 && a.Actor == b.Actor && a.NodeID == b.NodeID &&
		a.Repository == b.Repository && a.CanonicalRef == b.CanonicalRef && a.ExpectedHead == b.ExpectedHead &&
		a.Generation == b.Generation && a.BeforeNodeSHA256 == b.BeforeNodeSHA256 &&
		a.AfterNodeSHA256 == b.AfterNodeSHA256 && a.BeforePlanSHA256 == b.BeforePlanSHA256 &&
		a.AfterPlanSHA256 == b.AfterPlanSHA256 && a.BeforeReleaseSHA256 == b.BeforeReleaseSHA256 &&
		a.AfterReleaseSHA256 == b.AfterReleaseSHA256 && a.SnapshotSHA256 == b.SnapshotSHA256
}

func verifyPlanRephaseAfter(ioLayer planRephaseIO, nodePath, planPath, releasePath string, intent planRephaseIntent) error {
	for _, item := range []struct {
		path string
		sha  string
	}{
		{nodePath, intent.AfterNodeSHA256},
		{planPath, intent.AfterPlanSHA256},
		{releasePath, intent.AfterReleaseSHA256},
	} {
		raw, err := ioLayer.read(item.path)
		if err != nil || learnSHA(raw) != item.sha {
			return fmt.Errorf("after-generation differs: %s", filepath.Base(item.path))
		}
	}
	return nil
}

// verifyPlanRephaseCompletedReceipt rejects missing, substituted, or corrupted
// receipts before an idempotent replay may claim PASS.
// reconcilePreparedPlanRephase handles only the pre-event phase. A process
// can die between any two durable writes; every observed byte generation
// must match the intent-bound before/after hashes before rollback begins.
// Phases with possible native-event effects remain HOLD for manual reconcile.
func reconcilePreparedPlanRephase(ioLayer planRephaseIO, txDir, snapshotPath, intentPath string,
	intent planRephaseIntent, nodePath, planPath, releasePath string) error {
	if err := verifyPlanRephaseJournalPaths(filepath.Dir(txDir), txDir, intentPath, snapshotPath,
		filepath.Join(txDir, "node.before"), filepath.Join(txDir, "PLAN.before"), filepath.Join(txDir, "release-slices.before")); err != nil {
		return err
	}
	raw, err := os.ReadFile(snapshotPath)
	if err != nil {
		return err
	}
	if learnSHA(raw) != intent.SnapshotSHA256 {
		return fmt.Errorf("snapshot manifest SHA drift")
	}
	var meta planRephaseSnapshot
	if err := decodePlanRephaseJSON(raw, &meta); err != nil {
		return err
	}
	if meta.Schema != "air-worker.plan-rephase-snapshot/v1" ||
		meta.RequestID != intent.RequestID ||
		meta.NodeFile != "node.before" || meta.PlanFile != "PLAN.before" ||
		meta.ReleaseFile != "release-slices.before" ||
		meta.NodeSHA256 != intent.BeforeNodeSHA256 ||
		meta.PlanSHA256 != intent.BeforePlanSHA256 ||
		meta.ReleaseSHA256 != intent.BeforeReleaseSHA256 {
		return fmt.Errorf("snapshot identity mismatch")
	}
	files := make([]planRephaseFile, 0, 3)
	for _, item := range []struct {
		path, beforeName, beforeHash, afterHash string
	}{
		{nodePath, meta.NodeFile, intent.BeforeNodeSHA256, intent.AfterNodeSHA256},
		{planPath, meta.PlanFile, intent.BeforePlanSHA256, intent.AfterPlanSHA256},
		{releasePath, meta.ReleaseFile, intent.BeforeReleaseSHA256, intent.AfterReleaseSHA256},
	} {
		if !planRephaseSHA256.MatchString(item.beforeHash) || !planRephaseSHA256.MatchString(item.afterHash) {
			return fmt.Errorf("intent before/after hash absent")
		}
		before, err := os.ReadFile(filepath.Join(txDir, item.beforeName))
		if err != nil {
			return err
		}
		if learnSHA(before) != item.beforeHash {
			return fmt.Errorf("before snapshot hash mismatch for %s", item.beforeName)
		}
		current, err := ioLayer.read(item.path)
		if err != nil {
			return err
		}
		if learnSHA(current) != item.beforeHash && learnSHA(current) != item.afterHash {
			return fmt.Errorf("interrupted controlled file drift: %s", filepath.Base(item.path))
		}
		files = append(files, planRephaseFile{Path: item.path, Before: before, After: current})
	}
	// The preflight proved every current file is exactly an allowed generation.
	// Restore the persisted original bytes under the existing product lock.
	if err := rollbackPlanRephaseFiles(ioLayer, files); err != nil {
		return fmt.Errorf("prepared rollback unverified: %w", err)
	}
	intent.Phase = "recovered_rollback"
	if err := persistPlanRephaseIntent(intentPath, intent); err != nil {
		return fmt.Errorf("files restored, journal recovery persistence failed: %w", err)
	}
	return nil
}

func samePlanRephaseRefPair(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func verifyPlanRephaseCompletedReceipt(path string, intent planRephaseIntent, req planRephaseRequest) error {
	_, raw, err := readPlanRephaseEvidence(path)
	if err != nil {
		return err
	}
	if !planRephaseSHA256.MatchString(intent.ReceiptSHA256) || learnSHA(raw) != intent.ReceiptSHA256 {
		return fmt.Errorf("completed receipt exact SHA mismatch")
	}
	var receipt planRephaseReceipt
	if err := decodePlanRephaseJSON(raw, &receipt); err != nil {
		return err
	}
	if receipt.Schema != planRephaseReceiptSchema || receipt.Status != "PASS" ||
		receipt.RequestID != intent.RequestID || receipt.RequestSHA256 != intent.RequestSHA256 ||
		receipt.AuthorityReceiptSHA256 != intent.AuthoritySHA256 || receipt.Actor != intent.Actor ||
		receipt.Repository != intent.Repository || receipt.CanonicalRef != intent.CanonicalRef ||
		receipt.ObservedHead != intent.ExpectedHead || receipt.ObservedRemoteHead != intent.ExpectedHead ||
		receipt.Generation != intent.Generation || receipt.NodeID != intent.NodeID ||
		receipt.HistoryManifestSHA256 != req.HistoryManifestSHA256 ||
		receipt.HistoryManifestPath != req.HistoryManifestPath ||
		receipt.SourceProvenanceRef != req.SourceProvenanceRef ||
		receipt.SourceProvenanceCommit != req.SourceProvenanceCommit ||
		receipt.BeforeNodeSHA256 != intent.BeforeNodeSHA256 ||
		receipt.AfterNodeSHA256 != intent.AfterNodeSHA256 ||
		receipt.BeforePlanSHA256 != intent.BeforePlanSHA256 ||
		receipt.AfterPlanSHA256 != intent.AfterPlanSHA256 ||
		receipt.BeforeReleaseSHA256 != intent.BeforeReleaseSHA256 ||
		receipt.AfterReleaseSHA256 != intent.AfterReleaseSHA256 ||
		!samePlanRephaseRefPair(receipt.BeforeNodeRefs, req.Release.BeforeNodeRefs) ||
		!samePlanRephaseRefPair(receipt.AfterNodeRefs, req.Release.AfterNodeRefs) ||
		receipt.RollbackSnapshotSHA256 != intent.SnapshotSHA256 ||
		!receipt.StatusPreserved || !receipt.ReceiptsPreserved || receipt.SilentClosure ||
		receipt.ImportedHistoricalNodes != 0 ||
		!receipt.NoReleaseEffect || !receipt.NoInstallEffect || !receipt.NoProviderEffect {
		return fmt.Errorf("completed receipt identity or effect contract mismatch")
	}
	return nil
}

func planRephaseReject(asJSON bool, reason string, code int) int {
	if asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"schema": planRephaseReceiptSchema, "status": "HOLD", "reason": reason})
	} else {
		fmt.Fprintln(os.Stderr, "plan rephase HOLD:", reason)
	}
	return code
}

func planRephaseError(asJSON bool, reason string, err error, code int) int {
	if asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"schema": planRephaseReceiptSchema, "status": "HOLD", "reason": reason})
	} else {
		fmt.Fprintln(os.Stderr, "plan rephase HOLD:", reason+":", err)
	}
	return code
}
