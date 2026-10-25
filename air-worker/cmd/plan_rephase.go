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
	planRephaseAuthoritySchema = "air-worker.plan-rephase-authority/v1"
	planRephaseIntentSchema    = "air-worker.plan-rephase-intent/v1"
	planRephaseReceiptSchema   = "air-worker.plan-rephase-receipt/v1"
	planRephaseMaxEvidence     = 256 * 1024
)

var (
	planRephaseSHA40  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	planRephaseSHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)
	planRephaseRef    = regexp.MustCompile(`^refs/heads/[A-Za-z0-9][A-Za-z0-9._/-]{0,180}$`)
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

	if err := verifyPlanRephaseGitCAS(root, req); err != nil {
		return planRephaseError(*asJSON, "GIT_CAS_HOLD", err, 3)
	}

	nodePath, err := planNodePathByID(root, id)
	if err != nil {
		return planRephaseError(*asJSON, "NODE_NOT_FOUND", err, 3)
	}
	if filepath.Base(nodePath) != id+".md" {
		return planRephaseReject(*asJSON, "AMBIGUOUS_NODE_ID", 3)
	}
	planPath := filepath.Join(root, "PLAN.md")
	releasePath := filepath.Join(root, "plan", "release-slices.json")
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
	if priorRaw, readErr := os.ReadFile(intentPath); readErr == nil {
		var prior planRephaseIntent
		if json.Unmarshal(priorRaw, &prior) != nil || !samePlanRephaseIdentity(prior, strings.TrimSpace(*requestID), learnSHA(requestRaw), learnSHA(authorityRaw), eventActor, req) {
			return planRephaseReject(*asJSON, "REQUEST_ID_REBOUND", 3)
		}
		if prior.Phase != "complete" {
			return planRephaseReject(*asJSON, "INCOMPLETE_REPHASE_JOURNAL", 3)
		}
		if err := verifyPlanRephaseAfter(ioLayer, nodePath, planPath, releasePath, prior); err != nil {
			return planRephaseError(*asJSON, "COMPLETED_RECEIPT_STATE_MISMATCH", err, 3)
		}
		if _, err := os.Stat(receiptPath); err != nil {
			return planRephaseError(*asJSON, "COMPLETED_RECEIPT_MISSING", err, 3)
		}
		if *asJSON {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"schema": planRephaseReceiptSchema, "status": "PASS", "action": "already_rephased", "request_id": prior.RequestID, "receipt": receiptPath})
		}
		return 0
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return planRephaseError(*asJSON, "JOURNAL_READ_FAILED", readErr, 1)
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
	body, err := parsePlanNodeBody(nodePath, nodeBefore)
	if err != nil {
		return planRephaseError(*asJSON, "NODE_BODY_INVALID", err, 2)
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
	afterNode.UpdatedAt = changedAt.Format(time.RFC3339Nano)
	if err := validatePlanNode(afterNode); err != nil {
		return planRephaseError(*asJSON, "NODE_AFTER_INVALID", err, 2)
	}
	nodeAfter := renderPlanNodeWithBody(afterNode, body)
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
		return planRephaseError(*asJSON, "PUBLISH_ROLLED_BACK", err, 1)
	}
	intent.Phase = "event_pending"
	if err := persistPlanRephaseIntent(intentPath, intent); err != nil {
		_ = rollbackPlanRephaseFiles(ioLayer, files)
		return planRephaseError(*asJSON, "JOURNAL_AFTER_PUBLISH_FAILED", err, 1)
	}

	evidence := requestAbs + "#sha256=" + learnSHA(requestRaw) + ";authority=" + authorityAbs + "#sha256=" + learnSHA(authorityRaw)
	event := planNodeEventRowWithActor(afterNode, "rephased", evidence, "plan-rephase", changedAt, eventActor)
	if err := ioLayer.event(root, []map[string]any{event}); err != nil {
		_ = rollbackPlanRephaseFiles(ioLayer, files)
		intent.Phase = "event_failed_rolled_back"
		_ = persistPlanRephaseIntent(intentPath, intent)
		return planRephaseError(*asJSON, "EVENT_FAILED_ROLLED_BACK", err, 1)
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
		HistoryManifestSHA256: req.HistoryManifestSHA256,
		SourceProvenanceRef:   req.SourceProvenanceRef, SourceProvenanceCommit: req.SourceProvenanceCommit,
		NodeID: id, StatusPreserved: true, ReceiptsPreserved: true, SilentClosure: false,
		ImportedHistoricalNodes: 0, BeforeNodeSHA256: intent.BeforeNodeSHA256, AfterNodeSHA256: intent.AfterNodeSHA256,
		BeforePlanSHA256: intent.BeforePlanSHA256, AfterPlanSHA256: intent.AfterPlanSHA256,
		BeforeHeaderSHA256: req.Plan.HeaderBeforeSHA256, AfterHeaderSHA256: learnSHA([]byte(req.Plan.HeaderAfter)),
		BeforeReleaseSHA256: intent.BeforeReleaseSHA256, AfterReleaseSHA256: intent.AfterReleaseSHA256,
		BeforeParent: req.Node.BeforeParent, AfterParent: req.Node.AfterParent,
		BeforeTrigger: req.Node.BeforeTrigger, AfterTrigger: req.Node.AfterTrigger,
		BeforeDoneWhen: req.Node.BeforeDoneWhen, AfterDoneWhen: req.Node.AfterDoneWhen,
		ReleaseVersion: req.Release.Version, ReleaseStepID: req.Release.StepID,
		RollbackSnapshot: snapshotPath, RollbackSnapshotSHA256: snapshotSHA,
		ReadbackChecks:  []string{"node_sha", "plan_sha", "release_slices_sha", "native_event"},
		NoReleaseEffect: true, NoInstallEffect: true, NoProviderEffect: true,
		ChangedAt: changedAt.Format(time.RFC3339Nano),
	}
	if err := persistPlanRephaseReceipt(receiptPath, receipt); err != nil {
		return planRephaseError(*asJSON, "RECEIPT_WRITE_FAILED", err, 1)
	}
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
	if strings.TrimSpace(req.Generation) == "" || len(req.Generation) > 128 || strings.TrimSpace(req.SourceProvenanceRef) == "" {
		return fmt.Errorf("generation and source provenance ref are required")
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

func validatePlanRephaseNodeBefore(node planNode, want planRephaseNode) error {
	if node.ID != want.ID || node.Status != "open" || node.Owner != want.ExpectedOwner ||
		node.Parent != want.BeforeParent || node.ReturnTo != want.BeforeReturnTo ||
		node.Trigger != want.BeforeTrigger || node.DoneWhen != want.BeforeDoneWhen {
		return fmt.Errorf("node identity/status/owner/original fields differ from expected CAS")
	}
	return nil
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
	if !bytes.Equal(oldLine, newLine) {
		if bytes.Count(out, oldLine) != 1 {
			return nil, fmt.Errorf("PLAN spine has no unique exact node link")
		}
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
	_, afterView, err := findPlanRephaseStep(out, r.Version, r.StepID)
	if err != nil {
		return nil, fmt.Errorf("after release-slices validation: %w", err)
	}
	if afterView.Status != "open" || afterView.Trigger != r.AfterTrigger || afterView.DoneWhen != r.AfterDoneWhen {
		return nil, fmt.Errorf("after release step does not match requested rephase")
	}
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
	written := 0
	for i := range files {
		if err := ioLayer.write(files[i].Path, files[i].After); err != nil {
			for j := written - 1; j >= 0; j-- {
				_ = ioLayer.write(files[j].Path, files[j].Before)
			}
			return fmt.Errorf("publish %s: %w", filepath.Base(files[i].Path), err)
		}
		written++
	}
	for i := range files {
		got, err := ioLayer.read(files[i].Path)
		if err != nil || !bytes.Equal(got, files[i].After) {
			_ = rollbackPlanRephaseFiles(ioLayer, files)
			if err != nil {
				return fmt.Errorf("readback %s: %w", filepath.Base(files[i].Path), err)
			}
			return fmt.Errorf("readback mismatch for %s", filepath.Base(files[i].Path))
		}
	}
	return nil
}

func rollbackPlanRephaseFiles(ioLayer planRephaseIO, files []planRephaseFile) error {
	var first error
	for i := range files {
		if err := ioLayer.write(files[i].Path, files[i].Before); err != nil && first == nil {
			first = err
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
