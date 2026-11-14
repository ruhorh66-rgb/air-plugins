package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Synthetic verifier ONLY for transaction tests. It proves no real acceptance.
func deploymentFixture(t *testing.T) (runtimeDeploymentPlan, runtimeAcceptanceVerifier) {
	t.Helper()
	consumer := strings.Repeat("c", 64)
	bindings := map[string]runtimeDeploymentBinding{}
	makeSlot := func(id string) runtimeDeploymentSlot {
		artifact := []byte("fixture executable " + id)
		m := runtimeManifest{SchemaVersion: 1, ProductID: "air-worker", CapabilityID: "legacy-learning", InterfaceContract: "air-worker.legacy-learning/v1", ReleaseID: id, SourceRevision: strings.Repeat("a", 40), OwnerAccepted: true, Consumers: map[string]string{"air-curator": "accepted"}, Operations: []string{"check", "event", "review", "pending-read"}}
		m.Artifact.Path = "air-worker.exe"
		m.Artifact.SHA256 = runtimeDigest(artifact)
		manifest, e := json.Marshal(m)
		if e != nil {
			t.Fatal(e)
		}
		descriptor := []byte("TEST ONLY evidence " + id)
		bindings[string(descriptor)] = runtimeDeploymentBinding{id, m.SourceRevision, runtimeDigest(manifest), m.Artifact.SHA256, consumer}
		return runtimeDeploymentSlot{manifest, artifact, descriptor}
	}
	plan := runtimeDeploymentPlan{Current: makeSlot("new"), Previous: makeSlot("old"), ConsumerSHA256: consumer}
	verify := func(raw []byte) (runtimeDeploymentBinding, error) {
		b, ok := bindings[string(raw)]
		if !ok {
			return b, errors.New("unknown evidence")
		}
		return b, nil
	}
	return plan, verify
}
func TestRuntimeDeploymentPublishRollback(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(fmt.Sprint(exists), func(t *testing.T) {
			root := t.TempDir()
			before := []byte("prior exact bytes\n")
			if exists {
				if e := os.WriteFile(filepath.Join(root, "production.json"), before, 0600); e != nil {
					t.Fatal(e)
				}
			}
			plan, verify := deploymentFixture(t)
			tx, e := prepareRuntimeDeployment(root, plan, verify)
			if e != nil {
				t.Fatal(e)
			}
			if e = sameRuntimeDeploymentPointer(root, func() []byte {
				if exists {
					return before
				}
				return nil
			}(), exists); e != nil {
				t.Fatal(e)
			}
			// Caller mutation after prepare must not alter the accepted payload.
			plan.Current.Manifest[0] = '!'
			if e = tx.Publish(); e != nil {
				t.Fatal(e)
			}
			if _, e = resolveRuntime(root, "air-curator", "legacy-learning", "air-worker.legacy-learning/v1"); e != nil {
				t.Fatal(e)
			}
			if e = tx.Publish(); e != nil {
				t.Fatal(e)
			}
			if e = tx.Rollback(); e != nil {
				t.Fatal(e)
			}
			if e = tx.Rollback(); e != nil {
				t.Fatal(e)
			}
			if e = tx.Publish(); e == nil {
				t.Fatal("republished rolled-back transaction")
			}
		})
	}
}
func TestRuntimeDeploymentRejectsUnverifiedBeforeMutation(t *testing.T) {
	for _, kind := range []string{"nil", "error", "binding", "descriptor", "consumer", "artifact", "manifest", "operations", "same-release", "reserved-name"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			plan, verify := deploymentFixture(t)
			switch kind {
			case "nil":
				verify = nil
			case "error":
				verify = func([]byte) (runtimeDeploymentBinding, error) {
					return runtimeDeploymentBinding{}, errors.New("gate NOT_RUN")
				}
			case "binding":
				old := verify
				verify = func(b []byte) (runtimeDeploymentBinding, error) {
					v, e := old(b)
					v.ConsumerSHA256 = strings.Repeat("d", 64)
					return v, e
				}
			case "descriptor":
				plan.Previous.Descriptor = []byte("invented accepted")
			case "consumer":
				plan.ConsumerSHA256 = ""
			case "artifact":
				plan.Current.Artifact = []byte("tampered")
			case "manifest":
				plan.Current.Manifest = bytes.Replace(plan.Current.Manifest, []byte("new"), []byte("other"), 1)
			case "operations":
				plan.Current.Manifest = bytes.Replace(plan.Current.Manifest, []byte("review"), []byte("apply"), 1)
			case "same-release":
				plan.Previous = plan.Current
			case "reserved-name":
				plan.Current.Manifest = bytes.Replace(plan.Current.Manifest, []byte("air-worker.exe"), []byte("MANIFEST.JSON"), 1)
			}
			if _, e := prepareRuntimeDeployment(root, plan, verify); e == nil {
				t.Fatal("accepted invalid evidence/slot")
			}
			entries, e := os.ReadDir(root)
			if e != nil {
				t.Fatal(e)
			}
			if len(entries) != 0 {
				t.Fatal("invalid preparation mutated root")
			}
		})
	}
}
func TestRuntimeDeploymentCreateOnce(t *testing.T) {
	root := t.TempDir()
	plan, verify := deploymentFixture(t)
	if _, e := prepareRuntimeDeployment(root, plan, verify); e != nil {
		t.Fatal(e)
	}
	if _, e := prepareRuntimeDeployment(root, plan, verify); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(root, "releases", "old", "air-worker.exe")
	if e := os.WriteFile(p, []byte("corrupt"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := prepareRuntimeDeployment(root, plan, verify); e == nil {
		t.Fatal("repaired existing corrupt slot")
	}
	b, _ := os.ReadFile(p)
	if string(b) != "corrupt" {
		t.Fatal("existing slot overwritten")
	}
}
func TestRuntimeDeploymentRejectsPointerAndSlotDrift(t *testing.T) {
	for _, kind := range []string{"pointer-before", "slot-before", "evidence-before", "pointer-after"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			plan, verify := deploymentFixture(t)
			accepted := true
			gate := func(b []byte) (runtimeDeploymentBinding, error) {
				if !accepted {
					return runtimeDeploymentBinding{}, errors.New("acceptance withdrawn")
				}
				return verify(b)
			}
			tx, e := prepareRuntimeDeployment(root, plan, gate)
			if e != nil {
				t.Fatal(e)
			}
			pointer := filepath.Join(root, "production.json")
			other := []byte("another writer")
			switch kind {
			case "pointer-before":
				if e = os.WriteFile(pointer, other, 0600); e != nil {
					t.Fatal(e)
				}
			case "slot-before":
				if e = os.WriteFile(filepath.Join(root, "releases", "old", "manifest.json"), []byte("{}"), 0600); e != nil {
					t.Fatal(e)
				}
			case "evidence-before":
				accepted = false
			case "pointer-after":
				if e = tx.Publish(); e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(pointer, other, 0600); e != nil {
					t.Fatal(e)
				}
			}
			if kind == "pointer-after" {
				if e = tx.Rollback(); e == nil {
					t.Fatal("overwrote other writer")
				}
				b, _ := os.ReadFile(pointer)
				if !bytes.Equal(b, other) {
					t.Fatal("changed other pointer")
				}
			} else if e = tx.Publish(); e == nil {
				t.Fatal("published drifted state")
			}
		})
	}
}
func TestRuntimeDeploymentRejectsPhysicalAlias(t *testing.T) {
	root := t.TempDir()
	plan, verify := deploymentFixture(t)
	// Give both slots the same artifact bytes but distinct accepted manifests.
	plan.Previous.Artifact = append([]byte(nil), plan.Current.Artifact...)
	var m runtimeManifest
	_ = runtimeJSON(plan.Previous.Manifest, &m)
	m.Artifact.SHA256 = runtimeDigest(plan.Previous.Artifact)
	plan.Previous.Manifest, _ = json.Marshal(m)
	oldVerifier := verify
	verify = func(b []byte) (runtimeDeploymentBinding, error) {
		v, e := oldVerifier(b)
		if v.ReleaseID == "old" {
			v.ManifestSHA256 = runtimeDigest(plan.Previous.Manifest)
			v.ArtifactSHA256 = m.Artifact.SHA256
		}
		return v, e
	}
	tx, e := prepareRuntimeDeployment(root, plan, verify)
	if e != nil {
		t.Fatal(e)
	}
	old := filepath.Join(root, "releases", "old", "air-worker.exe")
	newPath := filepath.Join(root, "releases", "new", "air-worker.exe")
	if e = os.Remove(old); e != nil {
		t.Fatal(e)
	}
	if e = os.Link(newPath, old); e != nil {
		t.Skipf("hardlink unsupported: %v", e)
	}
	if e = tx.Publish(); e == nil {
		t.Fatal("published aliased slots")
	}
}
