package main

import (
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type learningDeliveryProof struct {
	Principal  string
	Session    string
	RunID      string
	ContextSHA string
}

func splitValidateLearningProof(argv []string) ([]string, *learningDeliveryProof, error) {
	ordinary := make([]string, 0, len(argv))
	values := map[string]string{}
	names := map[string]bool{"-learning-principal": true, "-learning-session-key": true, "-learning-run-id": true, "-learning-context-sha256": true}
	boundary := false
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		name, value, hasEq := strings.Cut(arg, "=")
		if strings.HasPrefix(name, "--") && names[name[1:]] {
			return nil, nil, fmt.Errorf("unsupported proof flag %s", name)
		}
		if boundary {
			if names[name] {
				return nil, nil, errors.New("learning proof flags must precede positional arguments and --")
			}
			ordinary = append(ordinary, arg)
			continue
		}
		if arg == "--" || !strings.HasPrefix(arg, "-") {
			boundary = true
			ordinary = append(ordinary, arg)
			continue
		}
		if (name == "-product" || name == "--product") && !hasEq {
			ordinary = append(ordinary, arg)
			i++
			if i < len(argv) {
				ordinary = append(ordinary, argv[i])
			}
			continue
		}
		if !names[name] {
			ordinary = append(ordinary, arg)
			continue
		}
		if _, exists := values[name]; exists {
			return nil, nil, fmt.Errorf("duplicate %s", name)
		}
		if !hasEq {
			i++
			if i >= len(argv) {
				return nil, nil, fmt.Errorf("missing %s value", name)
			}
			value = argv[i]
		}
		if value == "" || strings.HasPrefix(value, "-") {
			return nil, nil, fmt.Errorf("invalid %s value", name)
		}
		values[name] = value
	}
	if len(values) == 0 {
		return ordinary, nil, nil
	}
	if len(values) != 4 {
		return nil, nil, errors.New("all four learning proof flags are required")
	}
	id, err := parseIdentity(values["-learning-principal"], values["-learning-session-key"])
	if err != nil || id.Principal != values["-learning-principal"] || id.SessionKey != values["-learning-session-key"] {
		return nil, nil, errors.New("invalid learning identity")
	}
	run, err := sanitizeIdentityPart("run-id", values["-learning-run-id"])
	if err != nil || run != values["-learning-run-id"] {
		return nil, nil, errors.New("invalid learning run")
	}
	sha := values["-learning-context-sha256"]
	b, err := hex.DecodeString(sha)
	if err != nil || len(b) != 32 || strings.ToLower(sha) != sha {
		return nil, nil, errors.New("invalid learning context SHA")
	}
	return ordinary, &learningDeliveryProof{id.Principal, id.SessionKey, run, sha}, nil
}

func verifyCurrentLearningDelivery(owner selfLearningOwner, proof learningDeliveryProof, rule selfLearningLoadedRule) error {
	current, enabled, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !enabled || !sameLearningPath(current.Selector.ProductRoot, owner.Selector.ProductRoot) ||
		!sameLearningPath(current.Settings.RuntimeRoot, owner.Settings.RuntimeRoot) ||
		current.Selector.ConfigSHA256 != owner.Selector.ConfigSHA256 {
		return errors.New("learning delivery owner changed or disabled")
	}
	in := hookInput{Principal: proof.Principal, SessionID: proof.Session, RunID: proof.RunID}
	path, err := selfLearningContextReceiptPath(in)
	if err != nil {
		return err
	}
	raw, err := n093ReadContextEvidence(path)
	if err != nil {
		return err
	}
	if learnSHA(raw) != proof.ContextSHA {
		return errors.New("learning delivery is no longer current")
	}
	var rec selfLearningContextReceipt
	if err := json.Unmarshal(raw, &rec); err != nil {
		return err
	}
	if rec.Schema != selfLearningContextReceiptSchema || rec.Principal != proof.Principal || rec.Session != proof.Session || rec.RunID != proof.RunID ||
		!sameLearningPath(rec.ProductRoot, owner.Selector.ProductRoot) || !sameLearningPath(rec.RuntimeRoot, owner.Settings.RuntimeRoot) ||
		rec.ConfigSHA != owner.Selector.ConfigSHA256 || len(rec.Loaded) != 1 || rec.Loaded[0] != rule || len(rec.DeliveryID) != 32 || rec.DeliveryMAC == "" ||
		rec.UsedAt != "" || rec.Outcome != "" {
		return errors.New("invalid explicit learning delivery")
	}
	key, err := ownCLIPrivateKey(owner.Settings.RuntimeRoot)
	if err != nil {
		return err
	}
	expected, err := selfLearningDeliveryMAC(key, rec)
	for i := range key {
		key[i] = 0
	}
	if err != nil || !hmac.Equal([]byte(rec.DeliveryMAC), []byte(expected)) {
		return errors.New("learning delivery authentication failed")
	}
	return nil
}
