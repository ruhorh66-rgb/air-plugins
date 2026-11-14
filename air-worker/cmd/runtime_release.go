package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// This is an attestation by the existing release signing authority, not a
// locally generated PASS. Evidence bytes and all four exact gate results are
// signed. The publisher must supply real owner/consumer receipts.
type runtimeReleaseGate struct {
	Operation      string `json:"operation"`
	Outcome        string `json:"outcome"`
	EvidenceSHA256 string `json:"evidence_sha256"`
	Evidence       []byte `json:"evidence"`
}
type runtimeReleaseAcceptance struct {
	Manifest []byte               `json:"manifest"`
	Gates    []runtimeReleaseGate `json:"gates"`
}
type runtimeReleasePacket struct {
	Schema          string                   `json:"schema"`
	Version         string                   `json:"version"`
	SourceRevision  string                   `json:"source_revision"`
	BootstrapSHA256 string                   `json:"bootstrap_sha256"`
	PayloadSHA256   string                   `json:"payload_sha256"`
	ConsumerSHA256  string                   `json:"consumer_sha256"`
	Current         runtimeReleaseAcceptance `json:"current"`
	Previous        runtimeReleaseAcceptance `json:"previous"`
	SigningKeyID    string                   `json:"signing_key_id"`
	Signature       string                   `json:"signature"`
}

func runtimeReleaseSigningBytes(p runtimeReleasePacket) []byte {
	p.Signature = ""
	b, _ := json.Marshal(p)
	return append([]byte("air-worker.runtime-release/v1\n"), b...)
}
func verifyRuntimeReleasePacket(raw []byte, m updateManifest, consumerSHA, key string) (runtimeReleasePacket, error) {
	var p runtimeReleasePacket
	if len(raw) > maxUpdateManifestBytes {
		return p, errors.New("runtime release packet oversized")
	}
	if err := runtimeJSON(raw, &p); err != nil {
		return p, err
	}
	pub, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return p, errors.New("invalid runtime release verification key")
	}
	sig, err := base64.StdEncoding.DecodeString(p.Signature)
	if err != nil || p.SigningKeyID != updateKeyID || !ed25519.Verify(ed25519.PublicKey(pub), runtimeReleaseSigningBytes(p), sig) {
		return p, errors.New("runtime release signature invalid")
	}
	if p.Schema != "air-worker.runtime-release/v1" || p.Version != m.Version || p.SourceRevision != m.VCSRevision ||
		p.BootstrapSHA256 != m.CLI.SHA256 || p.PayloadSHA256 != m.PayloadSHA256 || !runtimeHash.MatchString(consumerSHA) || p.ConsumerSHA256 != consumerSHA {
		return p, errors.New("runtime release identity/installed consumer mismatch")
	}
	for _, a := range []runtimeReleaseAcceptance{p.Current, p.Previous} {
		var manifest runtimeManifest
		if err := runtimeJSON(a.Manifest, &manifest); err != nil {
			return p, err
		}
		required := map[string]bool{"check": false, "event": false, "review": false, "pending-read": false}
		for _, g := range a.Gates {
			seen, ok := required[g.Operation]
			if !ok || seen || g.Outcome != "PASS" || len(g.Evidence) == 0 || !runtimeHash.MatchString(g.EvidenceSHA256) || runtimeDigest(g.Evidence) != g.EvidenceSHA256 {
				return p, errors.New("runtime gate absent, non-PASS or evidence digest mismatch")
			}
			required[g.Operation] = true
		}
		for _, seen := range required {
			if !seen {
				return p, errors.New("runtime gate incomplete")
			}
		}
	}
	var current runtimeManifest
	if err := runtimeJSON(p.Current.Manifest, &current); err != nil {
		return p, err
	}
	if current.SourceRevision != m.VCSRevision || current.Artifact.SHA256 != m.CLI.SHA256 {
		return p, errors.New("current slot not the signed update CLI")
	}
	return p, nil
}

// Kept separate from schema-1 update manifests: old updaters re-encode those
// manifests and discard unknown fields. This asset is fetched by the NEW _apply.
func runtimeReleasePacketURL(m updateManifest) (string, error) {
	if err := validateUpdateArtifact(m.CLI, "air-worker-"+m.Version+"-windows-x64.exe", m.Version); err != nil {
		return "", err
	}
	return strings.TrimSuffix(m.CLI.URL, m.CLI.Name) + "air-worker-runtime-" + m.Version + ".json", nil
}
func fetchRuntimeReleasePacket(ctx context.Context, m updateManifest) ([]byte, bool, error) {
	u, err := runtimeReleasePacketURL(m)
	if err != nil {
		return nil, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false, err
	}
	response, err := updateHTTPClient(25 * time.Second).Do(req)
	if err != nil {
		return nil, false, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("runtime release asset HTTP %s", response.Status)
	}
	b, err := io.ReadAll(io.LimitReader(response.Body, maxUpdateManifestBytes+1))
	if err != nil {
		return nil, false, err
	}
	if len(b) > maxUpdateManifestBytes {
		return nil, false, errors.New("runtime packet oversized")
	}
	return b, true, nil
}
func runtimeOwnerRoot() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if !filepath.IsAbs(base) {
		return "", errors.New("LOCALAPPDATA unavailable")
	}
	return filepath.Join(base, "air-worker", "runtime"), nil
}

// Fixed installed-consumer location; never use a packet-supplied executable
// path, development checkout, environment override or old legacy adapter.
func installedRuntimeConsumerSHA() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if !filepath.IsAbs(base) {
		return "", errors.New("LOCALAPPDATA unavailable")
	}
	root := filepath.Join(base, "air-curator", "bin")
	p, err := runtimePath(root, "AirCuratorLearning.psm1")
	if err != nil {
		return "", fmt.Errorf("installed AirCurator consumer binding unavailable: %w", err)
	}
	s, err := os.Stat(p)
	if err != nil || !s.Mode().IsRegular() {
		return "", errors.New("installed consumer not regular")
	}
	return sha256File(p)
}
func runtimeReleasePlan(raw []byte, m updateManifest, consumerSHA string, current, previous []byte, key string) (runtimeDeploymentPlan, runtimeAcceptanceVerifier, error) {
	p, err := verifyRuntimeReleasePacket(raw, m, consumerSHA, key)
	if err != nil {
		return runtimeDeploymentPlan{}, nil, err
	}
	plan := runtimeDeploymentPlan{
		Current:        runtimeDeploymentSlot{Manifest: p.Current.Manifest, Artifact: current, Descriptor: raw},
		Previous:       runtimeDeploymentSlot{Manifest: p.Previous.Manifest, Artifact: previous, Descriptor: raw},
		ConsumerSHA256: consumerSHA,
	}
	// Each slot uses a distinct descriptor selecting an attested manifest.
	// The selection itself is not authority: the entire signed packet is rechecked.
	descriptor := func(which string) []byte {
		b, _ := json.Marshal(struct {
			Slot   string
			Packet []byte
		}{which, raw})
		return b
	}
	plan.Current.Descriptor = descriptor("current")
	plan.Previous.Descriptor = descriptor("previous")
	verify := func(d []byte) (runtimeDeploymentBinding, error) {
		var selector struct {
			Slot   string
			Packet []byte
		}
		if err := runtimeJSON(d, &selector); err != nil {
			return runtimeDeploymentBinding{}, err
		}
		if !bytes.Equal(selector.Packet, raw) {
			return runtimeDeploymentBinding{}, errors.New("runtime packet changed")
		}
		checked, err := verifyRuntimeReleasePacket(selector.Packet, m, consumerSHA, key)
		if err != nil {
			return runtimeDeploymentBinding{}, err
		}
		var a runtimeReleaseAcceptance
		switch selector.Slot {
		case "current":
			a = checked.Current
		case "previous":
			a = checked.Previous
		default:
			return runtimeDeploymentBinding{}, errors.New("unknown runtime slot")
		}
		var manifest runtimeManifest
		if err := runtimeJSON(a.Manifest, &manifest); err != nil {
			return runtimeDeploymentBinding{}, err
		}
		return runtimeDeploymentBinding{manifest.ReleaseID, manifest.SourceRevision, runtimeDigest(a.Manifest), manifest.Artifact.SHA256, checked.ConsumerSHA256}, nil
	}
	return plan, verify, nil
}
