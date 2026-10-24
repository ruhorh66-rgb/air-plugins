package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const selfLearningLoadSchema = "air-worker.self-learning.load/v1"

func cmdLearnSelfLoad(argv []string) int {
	fs := flag.NewFlagSet("learn self load", flag.ContinueOnError)
	principal := fs.String("principal", "", "explicit GPT principal")
	session := fs.String("session-key", "", "stable session key")
	run := fs.String("run-id", "", "explicit run id")
	target := fs.String("target", "", "catalog target relative to product")
	sha := fs.String("expected-sha256", "", "catalog SHA256")
	asJSON := fs.Bool("json", false, "machine-readable JSON")
	if err := fs.Parse(argv); err != nil || fs.NArg() != 0 || !*asJSON {
		fmt.Fprintln(os.Stderr, "usage: air-worker learn self load -principal <p> -session-key <k> -run-id <r> -target <relative target> -expected-sha256 <sha> -json")
		return 2
	}
	identity, err := parseIdentity(*principal, *session)
	var runID string
	if err == nil {
		runID, err = sanitizeIdentityPart("run-id", *run)
	}
	cleanTarget := filepath.ToSlash(filepath.Clean(filepath.FromSlash(*target)))
	if err == nil && (strings.TrimSpace(*target) == "" || cleanTarget != *target || filepath.IsAbs(*target) || cleanTarget == "." || cleanTarget == ".." || strings.HasPrefix(cleanTarget, "../")) {
		err = errors.New("target must be a clean relative catalog path")
	}
	if err == nil {
		var decoded []byte
		decoded, err = hex.DecodeString(*sha)
		if err == nil && (len(decoded) != 32 || strings.ToLower(*sha) != *sha) {
			err = errors.New("expected SHA256 must be 64 lowercase hex digits")
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn self load:", err)
		return 2
	}
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err == nil && !on {
		err = errors.New("self-learning owner is disabled")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn self load:", err)
		return 2
	}
	index, err := executeSharedLearning(owner.Selector.ProductRoot, owner.Settings, "index", nil)
	var catalog struct {
		Skills []sharedLearningCatalogSkill `json:"skills"`
	}
	if err == nil {
		err = json.Unmarshal(index.Data, &catalog)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn self load:", err)
		return 2
	}
	var selected *sharedLearningCatalogSkill
	for i := range catalog.Skills {
		if catalog.Skills[i].Target == cleanTarget && catalog.Skills[i].SHA256 == *sha {
			selected = &catalog.Skills[i]
			break
		}
	}
	if selected == nil {
		fmt.Fprintln(os.Stderr, "learn self load: target@SHA is absent from current verified APPLIED catalog")
		return 2
	}
	in := hookInput{Principal: identity.Principal, SessionID: identity.SessionKey, RunID: runID}
	if current, active, checkErr := activeAirWorkerSelfLearningOwner(); checkErr != nil || !active || current.Selector.ConfigSHA256 != owner.Selector.ConfigSHA256 || !sameLearningPath(current.Selector.ProductRoot, owner.Selector.ProductRoot) {
		fmt.Fprintln(os.Stderr, "learn self load: owner changed during delivery")
		return 2
	}
	result, err := sharedLearningContextFromCatalog(owner.Selector.ProductRoot, owner.Settings, in, runID, []sharedLearningCatalogSkill{*selected})
	if err != nil || len(result.LoadedSkills) != 1 || result.LoadedSkills[0].Target != cleanTarget || result.LoadedSkills[0].SHA256 != *sha {
		fmt.Fprintln(os.Stderr, "learn self load: requested procedure was not delivered", err)
		return 2
	}
	if current, active, checkErr := activeAirWorkerSelfLearningOwner(); checkErr != nil || !active || current.Selector.ConfigSHA256 != owner.Selector.ConfigSHA256 || !sameLearningPath(current.Selector.ProductRoot, owner.Selector.ProductRoot) {
		fmt.Fprintln(os.Stderr, "learn self load: owner changed during delivery")
		return 2
	}
	contextSHA, err := captureSelfLearningContext(owner, in, result.LoadedSkills, true)
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn self load:", err)
		return 2
	}
	path, err := selfLearningContextReceiptPath(in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn self load:", err)
		return 2
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
		"schema": selfLearningLoadSchema, "status": "loaded", "principal": identity.Principal,
		"session_key": identity.SessionKey, "run_id": runID, "target": cleanTarget,
		"sha256": *sha, "rule_id": cleanTarget + "@" + *sha,
		"context": result.Context, "context_receipt_path": path,
		"context_receipt_sha256": contextSHA,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "learn self load: write response:", err)
		return 2
	}
	return 0
}
