package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runtimePacketFixture(t *testing.T) (runtimeReleasePacket, updateManifest, ed25519.PrivateKey, string, []byte, []byte) {
	t.Helper()
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	plan, _ := deploymentFixture(t)
	m, _ := signedUpdateManifestForTest(t, "0.12.0", "stable")
	m.CLI.SHA256 = runtimeDigest(plan.Current.Artifact)
	m.CLI.Size = int64(len(plan.Current.Artifact))
	m.SigningKeyID = updateKeyID
	m.ManifestSig = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, updateSignatureMessage(m)))
	gates := func() []runtimeReleaseGate {
		var out []runtimeReleaseGate
		for _, op := range []string{"check", "event", "review", "pending-read"} {
			b := []byte("SYNTHETIC TEST ONLY " + op)
			out = append(out, runtimeReleaseGate{op, "PASS", runtimeDigest(b), b})
		}
		return out
	}
	p := runtimeReleasePacket{Schema: "air-worker.runtime-release/v1", Version: m.Version, SourceRevision: m.VCSRevision, BootstrapSHA256: m.CLI.SHA256, PayloadSHA256: m.PayloadSHA256, ConsumerSHA256: plan.ConsumerSHA256, Current: runtimeReleaseAcceptance{plan.Current.Manifest, gates()}, Previous: runtimeReleaseAcceptance{plan.Previous.Manifest, gates()}, SigningKeyID: updateKeyID}
	return p, m, priv, base64.StdEncoding.EncodeToString(pub), plan.Current.Artifact, plan.Previous.Artifact
}
func signRuntimePacketTest(t *testing.T, p runtimeReleasePacket, priv ed25519.PrivateKey) []byte {
	t.Helper()
	p.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, runtimeReleaseSigningBytes(p)))
	raw, e := json.Marshal(p)
	if e != nil {
		t.Fatal(e)
	}
	return raw
}
func TestRuntimeReleaseSignedBindings(t *testing.T) {
	p, m, priv, key, current, previous := runtimePacketFixture(t)
	raw := signRuntimePacketTest(t, p, priv)
	plan, verify, e := runtimeReleasePlan(raw, m, p.ConsumerSHA256, current, previous, key)
	if e != nil {
		t.Fatal(e)
	}
	root := t.TempDir()
	tx, e := prepareRuntimeDeployment(root, plan, verify)
	if e != nil {
		t.Fatal(e)
	}
	if e = tx.Publish(); e != nil {
		t.Fatal(e)
	}
	if _, e = resolveRuntime(root, "air-curator", "legacy-learning", "air-worker.legacy-learning/v1"); e != nil {
		t.Fatal(e)
	}
}
func TestRuntimeReleaseRejectsMissingTamperedAndReplayedGates(t *testing.T) {
	for _, kind := range []string{"signature", "source", "bootstrap", "payload", "consumer", "review-not-run", "missing-review", "duplicate", "evidence", "current-manifest", "previous-artifact", "unknown-field"} {
		t.Run(kind, func(t *testing.T) {
			p, m, priv, key, current, previous := runtimePacketFixture(t)
			consumer := p.ConsumerSHA256
			switch kind {
			case "source":
				p.SourceRevision = strings.Repeat("d", 40)
			case "bootstrap":
				p.BootstrapSHA256 = strings.Repeat("d", 64)
			case "payload":
				p.PayloadSHA256 = strings.Repeat("d", 64)
			case "consumer":
				consumer = strings.Repeat("d", 64)
			case "review-not-run":
				p.Previous.Gates[2].Outcome = "NOT_RUN"
			case "missing-review":
				p.Current.Gates = p.Current.Gates[:3]
			case "duplicate":
				p.Current.Gates[2] = p.Current.Gates[0]
			case "evidence":
				p.Current.Gates[0].Evidence = []byte("changed")
			case "current-manifest":
				p.Current.Manifest = p.Previous.Manifest
			case "previous-artifact":
				previous = []byte("wrong installed rollback")
			}
			raw := signRuntimePacketTest(t, p, priv)
			if kind == "signature" {
				raw[len(raw)-4] ^= 1
			}
			if kind == "unknown-field" {
				raw = append([]byte("{\"unauthorized\":true,"), raw[1:]...)
			}
			plan, verify, e := runtimeReleasePlan(raw, m, consumer, current, previous, key)
			if e == nil {
				_, e = prepareRuntimeDeployment(t.TempDir(), plan, verify)
			}
			if e == nil {
				t.Fatal("accepted invalid release packet")
			}
		})
	}
}
func TestRuntimeReleaseRequiresActualInstalledConsumer(t *testing.T) {
	base := t.TempDir()
	t.Setenv("LOCALAPPDATA", base)
	if _, e := installedRuntimeConsumerSHA(); e == nil {
		t.Fatal("missing consumer accepted")
	}
	dir := filepath.Join(base, "air-curator", "bin")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	b := []byte("TEST ONLY installed consumer")
	if e := os.WriteFile(filepath.Join(dir, "AirCuratorLearning.psm1"), b, 0600); e != nil {
		t.Fatal(e)
	}
	got, e := installedRuntimeConsumerSHA()
	if e != nil || got != runtimeDigest(b) {
		t.Fatalf("actual consumer: %s %v", got, e)
	}
}
func TestRuntimeReleaseURLUsesVerifiedTag(t *testing.T) {
	_, m, _, _, _, _ := runtimePacketFixture(t)
	u, e := runtimeReleasePacketURL(m)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.HasSuffix(u, "/air-worker-runtime-0.12.0.json") {
		t.Fatal(u)
	}
	m.CLI.URL = "https://github.com/ruhorh66-rgb/air-plugins/releases/latest/download/" + m.CLI.Name
	if _, e = runtimeReleasePacketURL(m); e == nil {
		t.Fatal("latest accepted")
	}
}
