package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// N-040 phase 1. A native parser can attest that a real operator requested
// an unsupported, previously costly help path. The request remains REJECTED;
// this receipt does NOT claim any learning use or prevented model billing.
// It is only a future eligibility witness for a separate compiled safe-route.
const (
	n040OpportunitySchema = "air-worker.n040.cli-help-opportunity/v1"
	n040HelpAction        = "air-worker.plan.unsupported-help/v1"
)

func n040RequestedUnsafeHelp(argv []string, root string) bool {
	// Exact full argv is mandatory. Parse-derived flags are not a valid
	// substitute: -dry-run/-model/etc. must never be signed as an absent flag.
	expected := n040HelpArgs(root)
	if len(argv) != len(expected) {
		return false
	}
	for i := range expected {
		if argv[i] != expected[i] {
			return false
		}
	}
	return true
}

func n040IDValid(id string) bool {
	if !strings.HasPrefix(id, "N040-HELP-") || len(id) != len("N040-HELP-")+32 {
		return false
	}
	_, err := hex.DecodeString(id[len("N040-HELP-"):])
	return err == nil
}

func n040OpportunityPath(owner selfLearningOwner, id string) (string, error) {
	if !n040IDValid(id) {
		return "", errors.New("invalid N040 opportunity id")
	}
	return filepath.Join(owner.Settings.RuntimeRoot, "n040-help-opportunities", id+".json"), nil
}

func n040OpportunityMAC(key []byte, fields map[string]string) string {
	clean := make(map[string]string, len(fields))
	for name, value := range fields {
		if name != "mac" {
			clean[name] = value
		}
	}
	body, _ := json.Marshal(clean)
	m := hmac.New(sha256.New, key)
	_, _ = m.Write([]byte("air-worker.n040.cli-help-opportunity/v1\x00"))
	_, _ = m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

func n040HelpArgs(root string) []string {
	// Full exact argv is never logged. A key-bound digest covers all tokens.
	return []string{"plan", "-product", root, "release", "-help"}
}

func n040RuntimeOpportunity(owner selfLearningOwner, argv []string, parserRC int) (map[string]string, string, error) {
	if parserRC != 2 || !n040RequestedUnsafeHelp(argv, owner.Selector.ProductRoot) {
		return nil, "", errors.New("N040 native completion did not confirm exact rejected parser request")
	}
	if strings.TrimSpace(owner.Selector.ProductRoot) == "" ||
		strings.TrimSpace(owner.Settings.RuntimeRoot) == "" ||
		owner.Settings.ProductID != "air-worker" {
		return nil, "", errors.New("N040 identity must have a real AirWorker owner/runtime")
	}
	if owner.Selector.ConfigSHA256 == "" {
		return nil, "", errors.New("N040 owner configuration has no pinned identity")
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, "", err
	}
	exeBytes, err := readLearningBounded(exe, 32*1024*1024)
	if err != nil {
		return nil, "", err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, "", err
	}
	id := "N040-HELP-" + hex.EncodeToString(nonce[:])
	path, err := n040OpportunityPath(owner, id)
	if err != nil {
		return nil, "", err
	}
	key, err := ownCLIPrivateKey(owner.Settings.RuntimeRoot)
	if err != nil {
		return nil, "", err
	}
	defer func() {
		for i := range key {
			key[i] = 0
		}
	}()
	actorSID, err := n040CurrentActorSID()
	if err != nil {
		return nil, "", err
	}
	instanceID, err := n040CurrentProcessInstance()
	if err != nil {
		return nil, "", err
	}
	receipt := map[string]string{
		"schema":                 n040OpportunitySchema,
		"id":                     id,
		"action":                 n040HelpAction,
		"kind":                   "native_unsupported_cli_help_attempt",
		"state":                  "PARSER_REJECTED_NO_MODEL",
		"parser_rc":              "2",
		"principal":              "air-worker-cli",
		"current_actor_sid":      actorSID,
		"process_instance_id":    instanceID,
		"product_id":             owner.Settings.ProductID,
		"product_root":           owner.Selector.ProductRoot,
		"runtime_root":           owner.Settings.RuntimeRoot,
		"owner_config_sha256":    owner.Selector.ConfigSHA256,
		"argv_hmac":              ownCLIInvocationMAC(key, argv),
		"binary_sha256":          learnSHA(exeBytes),
		"process_id":             fmt.Sprint(os.Getpid()),
		"observed_at_utc":        time.Now().UTC().Format(time.RFC3339Nano),
		"learning_use":           "NOT_PROVEN",
		"saved_paid_model_calls": "NOT_PROVEN",
	}
	receipt["mac"] = n040OpportunityMAC(key, receipt)
	b, err := json.Marshal(receipt)
	if err != nil {
		return nil, "", err
	}
	if err := writeReleaseReceiptNoReplace(path, append(b, '\n')); err != nil {
		return nil, "", err
	}
	ref := filepath.ToSlash(path) + "#sha256=" + learnSHA(append(b, '\n'))
	return receipt, ref, nil
}

// A follow-up native use will need this exact readback: HMAC authenticity,
// product owner and process identity, single regular file, exact bounded
// content, and an unexpired signed attempt. The readback itself is read-only.
func n040VerifyOpportunity(owner selfLearningOwner, id string, expectedSHA string) (map[string]string, error) {
	path, err := n040OpportunityPath(owner, id)
	if err != nil {
		return nil, err
	}
	st, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0 ||
		st.Size() < 1 || st.Size() > 8*1024 {
		return nil, errors.New("N040 receipt must be a bounded ordinary file")
	}
	if _, err := os.Readlink(path); err == nil {
		return nil, errors.New("N040 opportunity receipt cannot be a link")
	}
	b, err := readLearningBounded(path, 8*1024)
	if err != nil || learnSHA(b) != expectedSHA {
		return nil, errors.New("N040 receipt exact bytes are not independently matched")
	}
	var fields map[string]string
	if err := json.Unmarshal(b, &fields); err != nil {
		return nil, err
	}
	if len(fields) != 20 ||
		fields["schema"] != n040OpportunitySchema ||
		fields["id"] != id || fields["action"] != n040HelpAction ||
		fields["kind"] != "native_unsupported_cli_help_attempt" ||
		fields["state"] != "PARSER_REJECTED_NO_MODEL" ||
		fields["parser_rc"] != "2" ||
		fields["principal"] != "air-worker-cli" ||
		fields["current_actor_sid"] == "" ||
		len(fields["process_instance_id"]) != 32 ||
		fields["product_id"] != "air-worker" ||
		fields["product_root"] != owner.Selector.ProductRoot ||
		fields["runtime_root"] != owner.Settings.RuntimeRoot ||
		fields["owner_config_sha256"] != owner.Selector.ConfigSHA256 ||
		fields["learning_use"] != "NOT_PROVEN" ||
		fields["saved_paid_model_calls"] != "NOT_PROVEN" {
		return nil, errors.New("N040 signed opportunity identity/shape mismatch")
	}
	stamp, err := time.Parse(time.RFC3339Nano, fields["observed_at_utc"])
	if err != nil || stamp.After(time.Now().UTC().Add(90*time.Second)) ||
		time.Since(stamp) > 24*time.Hour {
		return nil, errors.New("N040 opportunity timestamp is stale or invalid")
	}
	if len(fields["binary_sha256"]) != 64 || fields["process_id"] == "" {
		return nil, errors.New("N040 missing native process/binary attribution")
	}
	if _, err := hex.DecodeString(fields["process_instance_id"]); err != nil {
		return nil, errors.New("N040 process generation must have a secure nonce")
	}
	currentSID, err := n040CurrentActorSID()
	if err != nil || fields["current_actor_sid"] != currentSID {
		return nil, errors.New("N040 signed source belongs to a different OS principal")
	}
	key, err := ownCLIPrivateKey(owner.Settings.RuntimeRoot)
	if err != nil {
		return nil, err
	}
	valid := hmac.Equal([]byte(fields["mac"]), []byte(n040OpportunityMAC(key, fields))) &&
		hmac.Equal([]byte(fields["argv_hmac"]), []byte(ownCLIInvocationMAC(key, n040HelpArgs(owner.Selector.ProductRoot))))
	for i := range key {
		key[i] = 0
	}
	if !valid {
		return nil, errors.New("N040 opportunity HMAC is invalid")
	}
	return fields, nil
}

// No global search, no model work, no synthetic proof: only the actual native
// parser may create the opportunity. It is not a substitute for N-034 effect.
// Use only with an explicit -product root equal to the ACTIVE AirWorker owner.
func n040MaybeCaptureUnsupportedHelp(argv []string, parserRC int) {
	// This callback is reached only after the native parser returned.
	// Require the exact five tokens; no flag alias or extra parameter.
	if parserRC != 2 || len(argv) != 5 || argv[0] != "plan" ||
		argv[1] != "-product" || argv[3] != "release" ||
		argv[4] != "-help" {
		return
	}
	owner, active, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !active || !n040RequestedUnsafeHelp(argv, owner.Selector.ProductRoot) {
		return
	}
	receipt, ref, err := n040RuntimeOpportunity(owner, argv, parserRC)
	if err != nil {
		fmt.Fprintln(os.Stderr, "N040 parser opportunity capture incomplete:", err)
		return
	}
	fmt.Fprintf(os.Stderr, "N040 parser-observed unsupported help: %s receipt=%s; parser RC2, no terminal OS exit or learning use claimed.\n", receipt["id"], ref)
}

// A signed opportunity is useful only if a second, genuinely separate native
// execution can select the reviewed skill before it does more work. It cannot
// be retroactively assigned to an already completed N039 result.
func n040ObservedUse(id, sha string) string {
	return n039Observed + " N040 actual unsupported-help request " + id +
		" receipt-sha256=" + sha +
		" resolved through approved documented help; avoided paid model cost NOT_PROVEN."
}

func n040ReadOpportunityForUse(owner selfLearningOwner, id string) (map[string]string, string, error) {
	path, err := n040OpportunityPath(owner, id)
	if err != nil {
		return nil, "", err
	}
	b, err := readLearningBounded(path, 8*1024)
	if err != nil {
		return nil, "", err
	}
	digest := learnSHA(b)
	receipt, err := n040VerifyOpportunity(owner, id, digest)
	if err != nil {
		return nil, "", err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, "", err
	}
	bin, err := readLearningBounded(exe, 64*1024*1024)
	if err != nil || learnSHA(bin) != receipt["binary_sha256"] {
		return nil, "", errors.New("N040 signed CLI opportunity belongs to a different source binary")
	}
	return receipt, digest, nil
}

func n040PreviouslyUsedOrPending(owner selfLearningOwner, id string) error {
	// The selection mutex must already be held; never call a model or
	// trust a plain PostToolUse claim as if it were this durable correlation.
	expected := "N040 actual unsupported-help request " + id + " receipt-sha256="
	if err := scanLearnJSONL(filepath.Join(owner.Settings.RuntimeRoot, "events.jsonl"), func(b []byte) error {
		var event map[string]any
		if err := json.Unmarshal(b, &event); err != nil {
			return err
		}
		if event["schema"] == "air.learning.event/v1" &&
			event["kind"] == "procedure_used" &&
			event["source"] == "native-plan-help" {
			text, _ := event["observed"].(string)
			if strings.Contains(text, expected) {
				return errors.New("N040 signed opportunity already credited to another use")
			}
		}
		return nil
	}); err != nil {
		return err
	}
	// A crash between immutable result and module event must NOT silently
	// consume the same opportunity with a new run ID. Require reconcile.
	dir := filepath.Join(owner.Settings.RuntimeRoot, "n039-safe-help")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) > 1000 {
		return errors.New("N040 pending-receipt scan exceeds bounded inventory")
	}
	for _, item := range entries {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".json") {
			continue
		}
		raw, err := readLearningBounded(filepath.Join(dir, item.Name()), 64*1024)
		if err != nil {
			return err
		}
		var rec map[string]string
		if err := json.Unmarshal(raw, &rec); err != nil {
			return err
		}
		if rec["opportunity_id"] == id {
			return errors.New("N040 opportunity has a prior immutable native result; reconcile before retry")
		}
	}
	return nil
}
