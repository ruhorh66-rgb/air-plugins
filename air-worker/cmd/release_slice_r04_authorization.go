package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// R04 cannot count a use performed before R04 OPEN. This source-owned
// HMAC authorization is created by the native release runner ONLY after the
// R03->R04 CAS. It does not add a module API or grant execution permissions.
const (
	releaseR04AuthorizationSchema = "air-worker.release-slice.r04-authorized-use/v1"
	releaseR04AuthorizationDomain = "air-worker.release-slice.r04-authorized-use/v1\x00"
)

type releaseR04Authorization struct {
	Schema             string `json:"schema"`
	ProductID          string `json:"product_id"`
	ProductRoot        string `json:"product_root"`
	OriginalRunID      string `json:"original_run_id"`
	OriginalEventID    string `json:"original_event_id"`
	RuleID             string `json:"rule_id"`
	CandidateBinarySHA string `json:"candidate_binary_sha256"`
	PlanSHA            string `json:"r04_open_plan_sha256"`
	R03ReceiptSHA      string `json:"r03_receipt_sha256"`
	CreatedUTC         string `json:"created_utc"`
	Signature          string `json:"hmac_sha256"`
}

func releaseR04AuthorizationMAC(key []byte, proof releaseR04Authorization) string {
	proof.Signature = ""
	raw, _ := json.Marshal(proof)
	m := hmac.New(sha256.New, key)
	_, _ = m.Write([]byte(releaseR04AuthorizationDomain))
	_, _ = m.Write(raw)
	return hex.EncodeToString(m.Sum(nil))
}
func releaseR04AuthorizationRef(sourceRun string) string {
	return "release-r04-authorizations/" + sourceRun + ".json"
}
func releaseR04ReadAuthorization(root string, owner selfLearningOwner, p r04Predecessor,
	plan []byte) (releaseR04Authorization, error) {
	var proof releaseR04Authorization
	id := p.Source.Prior.RunID
	if !n093SafeEventID(id) {
		return proof, errors.New("R04: unsafe original run identity")
	}
	raw, err := n093ReadRuntimeEvidence(owner.Settings.RuntimeRoot, releaseR04AuthorizationRef(id), 32*1024)
	if err != nil {
		return proof, err
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return proof, err
	}
	if err := json.Unmarshal(raw, &proof); err != nil {
		return proof, err
	}
	key, err := ownCLIPrivateKey(owner.Settings.RuntimeRoot)
	if err != nil {
		return proof, err
	}
	expected := releaseR04AuthorizationMAC(key, proof)
	n093ZeroKey(key)
	if proof.Signature == "" || !hmac.Equal([]byte(expected), []byte(proof.Signature)) {
		return proof, errors.New("R04: release-open authorization signature invalid")
	}
	if proof.Schema != releaseR04AuthorizationSchema ||
		proof.ProductID != "air-worker" || proof.ProductID != owner.Settings.ProductID ||
		!sameLearningPath(proof.ProductRoot, root) ||
		proof.OriginalRunID != id || proof.OriginalEventID != p.Source.SourceEventID ||
		proof.RuleID != p.Review.Target+"@"+p.Review.TargetSHA256 ||
		proof.CandidateBinarySHA != p.Source.BinarySHA ||
		proof.PlanSHA != learnSHA(plan) || proof.R03ReceiptSHA != p.R03SHA {
		return proof, errors.New("R04: authorization is stale, wrong owner/plan or wrong rule")
	}
	if _, err := time.Parse(time.RFC3339Nano, proof.CreatedUTC); err != nil {
		return proof, errors.New("R04: authorization has invalid timestamp")
	}
	return proof, nil
}
func releaseR04SealAuthorization(root string, owner selfLearningOwner,
	p r04Predecessor, plan []byte) error {
	if _, err := releaseR04State(root, inspectReleaseSlices(root), plan); err != nil {
		return err
	}
	path := filepath.Join(owner.Settings.RuntimeRoot, filepath.FromSlash(releaseR04AuthorizationRef(p.Source.Prior.RunID)))
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := n093CheckOrdinaryDir(dir); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return errors.New("R04: a prior signed authorization exists; no second use can start")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	proof := releaseR04Authorization{
		Schema: releaseR04AuthorizationSchema, ProductID: owner.Settings.ProductID,
		ProductRoot:   filepath.Clean(root),
		OriginalRunID: p.Source.Prior.RunID, OriginalEventID: p.Source.SourceEventID,
		RuleID:             p.Review.Target + "@" + p.Review.TargetSHA256,
		CandidateBinarySHA: p.Source.BinarySHA, PlanSHA: learnSHA(plan),
		R03ReceiptSHA: p.R03SHA, CreatedUTC: time.Now().UTC().Format(time.RFC3339Nano),
	}
	key, err := ownCLIPrivateKey(owner.Settings.RuntimeRoot)
	if err != nil {
		return err
	}
	proof.Signature = releaseR04AuthorizationMAC(key, proof)
	n093ZeroKey(key)
	b, err := json.MarshalIndent(proof, "", "  ")
	if err != nil {
		return err
	}
	if err := writeReleaseReceiptNoReplace(path, append(b, '\n')); err != nil {
		return err
	}
	got, err := releaseR04ReadAuthorization(root, owner, p, plan)
	if err != nil {
		return fmt.Errorf("R04: native authorization readback: %w", err)
	}
	if got != proof {
		return errors.New("R04: native authorization bytes changed")
	}
	return nil
}
