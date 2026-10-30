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
	"regexp"
	"strings"
	"time"
)

// The existing trusted UserPromptSubmit host hook is the ONLY issuer for a
// native plan-rephase grant. A worker-supplied approved:true JSON is NOT proof.
// No new service, privileged account or background daemon is created here.
// The installed host attestation and exact user approval remain separate gates.
const planRephaseGrantSchema = "air-worker.plan-rephase-userprompt-grant/v1"
const planRephaseGrantDomain = "air-worker.plan-rephase.userprompt-grant/v1\x00"

var (
	planRephaseGrantIDRE  = regexp.MustCompile(`^[A-Za-z0-9._-]{3,96}$`)
	planRephaseHookSource = verifyTrustedHostApprovalSource
	planRephaseNow        = time.Now
)

type planRephaseTrustedGrant struct {
	Schema          string `json:"schema"`
	RequestID       string `json:"request_id"`
	RequestSHA256   string `json:"request_sha256"`
	AuthoritySHA256 string `json:"authority_sha256"`
	Generation      string `json:"generation"`
	ProductRoot     string `json:"product_root"`
	Actor           string `json:"actor"`
	Principal       string `json:"principal"`
	SessionKey      string `json:"session_key"`
	PromptSHA256    string `json:"prompt_sha256"`
	CreatedUTC      string `json:"created_utc"`
	GrantMAC        string `json:"grant_mac"`
}

func planRephaseGrantRuntime(root string) (string, error) {
	settings, shared, err := readSharedLearningSettings(root)
	if err != nil {
		return "", err
	}
	if shared {
		return settings.RuntimeRoot, nil
	}
	return filepath.Join(root, ".air-worker"), nil
}

func planRephaseGrantPath(runtimeRoot, requestID string) string {
	return filepath.Join(runtimeRoot, "plan-rephase", "lpr-grants", learnSHA([]byte(requestID))+".json")
}

func parsePlanRephaseUserApproval(prompt string) (requestID, requestSHA, authoritySHA, generation string, matched bool, err error) {
	text := strings.TrimSpace(prompt)
	if !strings.HasPrefix(text, "да N124") {
		return "", "", "", "", false, nil
	}
	fields := strings.Fields(text)
	if len(fields) != 6 || fields[0] != "да" || fields[1] != "N124" ||
		strings.Join(fields, " ") != text {
		return "", "", "", "", true, errors.New("N124 requires exact 'да N124 <request-id> <request-sha256> <authority-sha256> <generation>'")
	}
	id, rq, au, gen := fields[2], fields[3], fields[4], fields[5]
	if !planRephaseGrantIDRE.MatchString(id) || !planRephaseSHA256.MatchString(rq) ||
		!planRephaseSHA256.MatchString(au) || len(gen) > 128 {
		return "", "", "", "", true, errors.New("N124 exact grant fields are invalid")
	}
	if _, err := sanitizeIdentityPart("generation", gen); err != nil {
		return "", "", "", "", true, errors.New("N124 generation is not canonical")
	}
	return id, rq, au, gen, true, nil
}

func planRephaseGrantActor(in hookInput) (string, error) {
	id, ok := hookInputIdentity(in)
	if !ok {
		return "", errors.New("N124 hook lacks authenticated session identity")
	}
	// This minimal release binds only the GPT adapter that validates the
	// AIR Commander parent marker. Raw Claude CLI input never issues grants.
	if id.Principal != "gpt" && id.Principal != "chatgpt" {
		return "", errors.New("N124 issuer requires trusted GPT host principal")
	}
	if !in.HostTrusted || in.HookEventName != "UserPromptSubmit" ||
		!sessionActiveInput(in) {
		return "", errors.New("N124 issuer needs active trusted UserPromptSubmit")
	}
	if err := planRephaseHookSource(); err != nil {
		return "", fmt.Errorf("N124 issuer transport not attested: %w", err)
	}
	return "gpt-window:" + id.SessionKey, nil
}

func planRephaseGrantMAC(key []byte, grant planRephaseTrustedGrant) (string, error) {
	grant.GrantMAC = ""
	b, err := json.Marshal(grant)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(planRephaseGrantDomain))
	_, _ = mac.Write(b)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func planRephaseGrantKey(runtimeRoot string) ([]byte, error) {
	// Reuse the source-owned Windows DPAPI-sealed native key. Its lifecycle
	// is serialized by the already existing self-cli-failure mutex.
	lock, ok := acquireLock(lockName("self-cli-failure", runtimeRoot))
	if !ok {
		return nil, errors.New("N124 grant key writer is busy")
	}
	defer lock.release()
	return ownCLIPrivateKey(runtimeRoot)
}

func planRephaseWriteGrant(path string, raw []byte) error {
	// Immutable CREATE_NEW, not a replaceable user-supplied authority file.
	// Check EXISTING ancestors before MkdirAll; it must not create a child
	// through a junction/symlink, even on the error path. Check again after.
	dir := filepath.Dir(path)
	for current := filepath.Clean(dir); ; {
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("N124 grant path has linked ancestor: %s", current)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := checkPlanRephaseNoLinkedAncestors(dir); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(raw); err != nil {
		return err
	}
	return f.Sync()
}

// capturePlanRephaseUserPromptGrant is called exclusively from the existing
// UserPromptSubmit lifecycle. It does not approve a release or execute CAS.
// It captures the LPR's future exact words; no such grant is minted during
// development, testing on real products or by a model-supplied CLI argument.
func capturePlanRephaseUserPromptGrant(root string, in hookInput) (bool, error) {
	requestID, requestSHA, authoritySHA, generation, matched, err := parsePlanRephaseUserApproval(in.Prompt)
	if !matched || err != nil {
		return matched, err
	}
	actor, err := planRephaseGrantActor(in)
	if err != nil {
		return true, err
	}
	product, err := normalizePlanNodeProduct(root)
	if err != nil {
		return true, err
	}
	runtimeRoot, err := planRephaseGrantRuntime(product)
	if err != nil {
		return true, err
	}
	now := planRephaseNow().UTC()
	identity, _ := hookInputIdentity(in)
	grant := planRephaseTrustedGrant{
		Schema:    planRephaseGrantSchema,
		RequestID: requestID, RequestSHA256: requestSHA,
		AuthoritySHA256: authoritySHA, Generation: generation,
		ProductRoot: product, Actor: actor,
		Principal: identity.Principal, SessionKey: identity.SessionKey,
		PromptSHA256: learnSHA([]byte(strings.TrimSpace(in.Prompt))),
		CreatedUTC:   now.Format(time.RFC3339Nano),
	}
	key, err := planRephaseGrantKey(runtimeRoot)
	if err != nil {
		return true, err
	}
	grant.GrantMAC, err = planRephaseGrantMAC(key, grant)
	for i := range key {
		key[i] = 0
	}
	if err != nil {
		return true, err
	}
	payload, err := json.MarshalIndent(grant, "", "  ")
	if err != nil {
		return true, err
	}
	path := planRephaseGrantPath(runtimeRoot, requestID)
	if err := planRephaseWriteGrant(path, append(payload, '\n')); err != nil {
		return true, fmt.Errorf("N124 trusted grant not committed: %w", err)
	}
	return true, nil
}

func verifyPlanRephaseTrustedGrant(root string, req planRephaseRequest, auth planRephaseAuthority, authorityRaw []byte, actor, requestSHA, requestID string) error {
	if !planRephaseGrantIDRE.MatchString(requestID) {
		return errors.New("N124 invalid exact request identity")
	}
	runtimeRoot, err := planRephaseGrantRuntime(root)
	if err != nil {
		return err
	}
	path := planRephaseGrantPath(runtimeRoot, requestID)
	_, raw, err := readPlanRephaseEvidence(path)
	if err != nil {
		return fmt.Errorf("N124 signed trusted user grant unavailable: %w", err)
	}
	var grant planRephaseTrustedGrant
	if err := decodePlanRephaseJSON(raw, &grant); err != nil {
		return err
	}
	if grant.Schema != planRephaseGrantSchema ||
		grant.RequestID != requestID || grant.RequestSHA256 != requestSHA ||
		grant.AuthoritySHA256 != learnSHA(authorityRaw) ||
		grant.Generation != req.Generation || grant.Actor != actor ||
		grant.Principal != "gpt" && grant.Principal != "chatgpt" ||
		actor != "gpt-window:"+grant.SessionKey ||
		!sameLearningPath(grant.ProductRoot, root) ||
		grant.GrantMAC == "" || grant.PromptSHA256 == "" {
		return errors.New("N124 user grant request/actor/generation/authority scope mismatch")
	}
	// A durable grant is not an eternal session capability. The same declared
	// principal/session/product must still be active at the native apply boundary.
	id, err := parseIdentity(grant.Principal, grant.SessionKey)
	if err != nil {
		return err
	}
	var mode sessionModeState
	if err := readJSON(sessionModePath(hookStateDir(), id), &mode); err != nil ||
		!mode.Enabled || mode.Principal != id.Principal || mode.SessionKey != id.SessionKey {
		return errors.New("N124 original trusted host session is inactive or changed")
	}
	var product sessionProductState
	if err := readJSON(sessionProductPath(hookStateDir(), id), &product); err != nil ||
		product.Principal != id.Principal || product.SessionKey != id.SessionKey ||
		!sameLearningPath(product.Path, root) {
		return errors.New("N124 original session product binding is no longer current")
	}
	// No user consent is transferable to another exact request or indefinitely.
	at, err := time.Parse(time.RFC3339Nano, grant.CreatedUTC)
	if err != nil || at.After(planRephaseNow().UTC().Add(5*time.Minute)) ||
		planRephaseNow().UTC().Sub(at) > 24*time.Hour {
		return errors.New("N124 user grant expired or has invalid time")
	}
	// Auth.Approved is only an untrusted field. A host-issued, sealed,
	// request-ID-bound grant is independently required in addition.
	key, err := planRephaseGrantKey(runtimeRoot)
	if err != nil {
		return err
	}
	mac, err := planRephaseGrantMAC(key, grant)
	for i := range key {
		key[i] = 0
	}
	if err != nil || !hmac.Equal([]byte(mac), []byte(grant.GrantMAC)) {
		return errors.New("N124 trusted user grant MAC invalid")
	}
	_ = auth // legacy authority digest already verified by caller
	return nil
}
