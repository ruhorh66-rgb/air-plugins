package main

// Preparation only. Production integration MUST hold the native install lock,
// establish an owner-controlled non-reparse runtime root, and provide the
// existing owner/consumer evidence verifier. This helper neither grants
// permissions nor decides first-install/rollback acceptance policy.
// Rollback below is in-process compensation, NOT crash recovery.
import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type runtimeDeploymentBinding struct {
	ReleaseID      string
	SourceRevision string
	ManifestSHA256 string
	ArtifactSHA256 string
	ConsumerSHA256 string
}

// Descriptor is opaque to this transaction. The verifier must validate actual
// owner AND consumer gate evidence (all four operations), its authority and
// freshness, then return its exact bindings. JSON flags alone are not evidence.
// No production verifier or acceptance-producing implementation is supplied.
type runtimeAcceptanceVerifier func(descriptor []byte) (runtimeDeploymentBinding, error)

type runtimeDeploymentSlot struct {
	Manifest   []byte
	Artifact   []byte
	Descriptor []byte
}
type runtimeDeploymentPlan struct {
	Current        runtimeDeploymentSlot
	Previous       runtimeDeploymentSlot
	ConsumerSHA256 string
}
type runtimeDeploymentTransaction struct {
	root           string
	before         []byte
	existed        bool
	after          []byte
	published      bool
	rolledBack     bool
	consumerSHA256 string
	current        runtimeDeploymentSlot
	previous       runtimeDeploymentSlot
	verify         runtimeAcceptanceVerifier
}

func validateRuntimeDeploymentSlot(slot runtimeDeploymentSlot, consumerSHA string, verify runtimeAcceptanceVerifier) (runtimeManifest, error) {
	var m runtimeManifest
	if verify == nil || !runtimeHash.MatchString(consumerSHA) || len(slot.Descriptor) == 0 {
		return m, errors.New("verified acceptance descriptor and exact consumer SHA-256 required")
	}
	if err := runtimeJSON(slot.Manifest, &m); err != nil {
		return m, err
	}
	if m.SchemaVersion != 1 || m.ProductID != "air-worker" || m.CapabilityID != "legacy-learning" ||
		m.InterfaceContract != "air-worker.legacy-learning/v1" || !runtimeComponent(m.ReleaseID) ||
		!runtimeRevision.MatchString(m.SourceRevision) || !runtimeComponent(m.Artifact.Path) ||
		strings.EqualFold(m.Artifact.Path, "manifest.json") || strings.EqualFold(m.Artifact.Path, "acceptance.json") ||
		!m.OwnerAccepted || len(m.Consumers) != 1 || m.Consumers["air-curator"] != "accepted" ||
		!runtimeHash.MatchString(m.Artifact.SHA256) || runtimeDigest(slot.Artifact) != m.Artifact.SHA256 {
		return m, errors.New("invalid runtime slot identity")
	}
	required := map[string]bool{"check": false, "event": false, "review": false, "pending-read": false}
	for _, op := range m.Operations {
		seen, ok := required[op]
		if !ok || seen {
			return m, errors.New("invalid runtime operations")
		}
		required[op] = true
	}
	for _, seen := range required {
		if !seen {
			return m, errors.New("missing runtime operation")
		}
	}
	b, err := verify(append([]byte(nil), slot.Descriptor...))
	if err != nil {
		return m, fmt.Errorf("acceptance verification: %w", err)
	}
	want := runtimeDeploymentBinding{m.ReleaseID, m.SourceRevision, runtimeDigest(slot.Manifest), m.Artifact.SHA256, consumerSHA}
	if b != want {
		return m, errors.New("acceptance descriptor binding mismatch")
	}
	return m, nil
}

func readRuntimeDeploymentPointer(root string) ([]byte, bool, error) {
	path := filepath.Join(root, "production.json")
	_, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if _, err = runtimePath(root, "production.json"); err != nil {
		return nil, false, err
	}
	b, err := runtimeRead(path)
	return b, true, err
}
func sameRuntimeDeploymentPointer(root string, want []byte, existed bool) error {
	b, exists, err := readRuntimeDeploymentPointer(root)
	if err != nil {
		return err
	}
	if exists != existed || !bytes.Equal(b, want) {
		return errors.New("runtime pointer changed; refusing overwrite")
	}
	return nil
}

func runtimeSlotBytes(root string, m runtimeManifest, slot runtimeDeploymentSlot) error {
	for _, item := range []struct {
		name string
		data []byte
	}{
		{"manifest.json", slot.Manifest}, {m.Artifact.Path, slot.Artifact}, {"acceptance.json", slot.Descriptor},
	} {
		p, err := runtimePath(root, "releases", m.ReleaseID, item.name)
		if err != nil {
			return err
		}
		s, err := os.Stat(p)
		if err != nil {
			return err
		}
		if !s.Mode().IsRegular() || s.Size() != int64(len(item.data)) {
			return errors.New("existing runtime slot differs")
		}
		got, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if !bytes.Equal(got, item.data) {
			return errors.New("existing runtime slot differs")
		}
	}
	return nil
}
func createRuntimeDeploymentSlot(root string, m runtimeManifest, slot runtimeDeploymentSlot) error {
	releases, err := runtimePath(root, "releases")
	if err != nil {
		return err
	}
	dest := filepath.Join(releases, m.ReleaseID)
	if _, err := os.Lstat(dest); err == nil {
		return runtimeSlotBytes(root, m, slot)
	} else if !os.IsNotExist(err) {
		return err
	}
	stage, err := os.MkdirTemp(releases, "prepare-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for _, item := range []struct {
		name string
		data []byte
	}{
		{"manifest.json", slot.Manifest}, {m.Artifact.Path, slot.Artifact}, {"acceptance.json", slot.Descriptor},
	} {
		f, err := os.OpenFile(filepath.Join(stage, item.name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, werr := f.Write(item.data)
		if werr == nil {
			werr = f.Sync()
		}
		cerr := f.Close()
		if werr != nil {
			return werr
		}
		if cerr != nil {
			return cerr
		}
	}
	// Caller serialization and protected parents are required for create-once.
	// Never merge into or repair an existing release directory.
	if _, err := os.Lstat(dest); err == nil {
		return errors.New("runtime slot appeared during preparation")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err = os.Rename(stage, dest); err != nil {
		return err
	}
	return runtimeSlotBytes(root, m, slot)
}
func cloneRuntimeDeploymentSlot(s runtimeDeploymentSlot) runtimeDeploymentSlot {
	return runtimeDeploymentSlot{append([]byte(nil), s.Manifest...), append([]byte(nil), s.Artifact...), append([]byte(nil), s.Descriptor...)}
}

// prepareRuntimeDeployment leaves production.json untouched. Unreferenced,
// validated slots may remain after an aborted preparation; no cleanup of
// accepted slots is attempted. Root must already exist under owner control.
func prepareRuntimeDeployment(root string, plan runtimeDeploymentPlan, verify runtimeAcceptanceVerifier) (*runtimeDeploymentTransaction, error) {
	plan.Current = cloneRuntimeDeploymentSlot(plan.Current)
	plan.Previous = cloneRuntimeDeploymentSlot(plan.Previous)
	current, err := validateRuntimeDeploymentSlot(plan.Current, plan.ConsumerSHA256, verify)
	if err != nil {
		return nil, err
	}
	previous, err := validateRuntimeDeploymentSlot(plan.Previous, plan.ConsumerSHA256, verify)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(current.ReleaseID, previous.ReleaseID) {
		return nil, errors.New("current and previous release must differ")
	}
	if _, err = runtimePath(root); err != nil {
		return nil, err
	}
	before, existed, err := readRuntimeDeploymentPointer(root)
	if err != nil {
		return nil, err
	}
	if err = os.Mkdir(filepath.Join(root, "releases"), 0700); err != nil && !os.IsExist(err) {
		return nil, err
	}
	if _, err = runtimePath(root, "releases"); err != nil {
		return nil, err
	}
	for _, v := range []struct {
		m runtimeManifest
		s runtimeDeploymentSlot
	}{{previous, plan.Previous}, {current, plan.Current}} {
		if err = createRuntimeDeploymentSlot(root, v.m, v.s); err != nil {
			return nil, err
		}
	}
	p := runtimePointer{1, "air-worker", current.ReleaseID, runtimeDigest(plan.Current.Manifest), true, previous.ReleaseID}
	after, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	after = append(after, '\n')
	return &runtimeDeploymentTransaction{root: root, before: before, existed: existed, after: after, consumerSHA256: plan.ConsumerSHA256, current: plan.Current, previous: plan.Previous, verify: verify}, nil
}

func (t *runtimeDeploymentTransaction) Publish() error {
	if t.rolledBack {
		return errors.New("transaction already rolled back")
	}
	if t.published {
		return sameRuntimeDeploymentPointer(t.root, t.after, true)
	}
	if err := sameRuntimeDeploymentPointer(t.root, t.before, t.existed); err != nil {
		return err
	}
	for _, slot := range []runtimeDeploymentSlot{t.current, t.previous} {
		m, err := validateRuntimeDeploymentSlot(slot, t.consumerSHA256, t.verify)
		if err != nil {
			return err
		}
		if err = runtimeSlotBytes(t.root, m, slot); err != nil {
			return err
		}
	}
	cm, _, cp, err := runtimeSlot(t.root, mustRuntimeDeploymentID(t.current), runtimeDigest(t.current.Manifest), "air-curator", "legacy-learning", "air-worker.legacy-learning/v1")
	if err != nil {
		return err
	}
	_, _, pp, err := runtimeSlot(t.root, mustRuntimeDeploymentID(t.previous), runtimeDigest(t.previous.Manifest), "air-curator", "legacy-learning", "air-worker.legacy-learning/v1")
	if err != nil {
		return err
	}
	_ = cm
	for _, pair := range [][2]string{{cp, pp}, {filepath.Dir(cp), filepath.Dir(pp)}} {
		a, e := os.Stat(pair[0])
		if e != nil {
			return e
		}
		b, e := os.Stat(pair[1])
		if e != nil {
			return e
		}
		if os.SameFile(a, b) {
			return errors.New("runtime slots alias the same physical target")
		}
	}
	// Mark intent before the atomic writer: an error may occur after replacement.
	// Rollback reconciles the actual pointer; it never blindly overwrites a third party.
	t.published = true
	if err := writeFileAtomicDurable(filepath.Join(t.root, "production.json"), t.after); err != nil {
		return err
	}
	return sameRuntimeDeploymentPointer(t.root, t.after, true)
}
func (t *runtimeDeploymentTransaction) Rollback() error {
	if t.rolledBack {
		return sameRuntimeDeploymentPointer(t.root, t.before, t.existed)
	}
	if !t.published {
		t.rolledBack = true
		return sameRuntimeDeploymentPointer(t.root, t.before, t.existed)
	}
	if err := sameRuntimeDeploymentPointer(t.root, t.before, t.existed); err == nil {
		t.rolledBack = true
		return nil
	}
	if err := sameRuntimeDeploymentPointer(t.root, t.after, true); err != nil {
		return err
	}
	var err error
	if t.existed {
		err = writeFileAtomicDurable(filepath.Join(t.root, "production.json"), t.before)
	} else {
		err = os.Remove(filepath.Join(t.root, "production.json"))
	}
	if err != nil {
		return err
	}
	if err = sameRuntimeDeploymentPointer(t.root, t.before, t.existed); err != nil {
		return err
	}
	t.rolledBack = true
	return nil
}

func mustRuntimeDeploymentID(s runtimeDeploymentSlot) string {
	var m runtimeManifest
	_ = runtimeJSON(s.Manifest, &m) // caller already validates the frozen manifest
	return m.ReleaseID
}
