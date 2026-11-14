//go:build windows

package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func runtimeIntentFixture(t *testing.T) (nativeRuntimeIntent, string) {
	t.Helper()
	p, m, priv, key, _, previous := runtimePacketFixture(t)
	var currentManifest, previousManifest runtimeManifest
	_ = runtimeJSON(p.Current.Manifest, &currentManifest)
	_ = runtimeJSON(p.Previous.Manifest, &previousManifest)
	after, _ := json.Marshal(runtimePointer{1, "air-worker", currentManifest.ReleaseID, runtimeDigest(p.Current.Manifest), true, previousManifest.ReleaseID})
	return nativeRuntimeIntent{Schema: "air-worker.runtime-update-intent/v1", SnapshotID: "20261114T120000.000Z", Manifest: m, Packet: signRuntimePacketTest(t, p, priv), After: append(after, '\n'), OldCLI: runtimeDigest(previous), OldTray: runtimeDigest([]byte("old tray"))}, key
}
func TestNativeRuntimeIntentAuthentication(t *testing.T) {
	for _, kind := range []string{"valid", "packet", "manifest", "snapshot", "pointer", "rollback", "absence"} {
		t.Run(kind, func(t *testing.T) {
			intent, key := runtimeIntentFixture(t)
			switch kind {
			case "packet":
				intent.Packet = []byte("{}")
			case "manifest":
				intent.Manifest.CLI.SHA256 = runtimeDigest([]byte("other"))
			case "snapshot":
				intent.SnapshotID = "../outside"
			case "pointer":
				intent.After = []byte("{}")
			case "rollback":
				intent.OldCLI = runtimeDigest([]byte("other"))
			case "absence":
				intent.Before = []byte("{}")
			}
			raw, _ := json.Marshal(intent)
			_, e := decodeNativeRuntimeIntent(raw, key)
			if kind == "valid" && e != nil {
				t.Fatal(e)
			}
			if kind != "valid" && e == nil {
				t.Fatal("invalid intent accepted")
			}
		})
	}
}
func TestNativeRuntimeDurablePointerRecovery(t *testing.T) {
	for _, published := range []bool{false, true} {
		for _, existed := range []bool{false, true} {
			root := t.TempDir()
			before := []byte("previous pointer bytes")
			after := []byte("published pointer bytes")
			if existed {
				if e := writeFileAtomicDurable(filepath.Join(root, "production.json"), before); e != nil {
					t.Fatal(e)
				}
			}
			intent := nativeRuntimeIntent{Schema: "TEST ONLY", Before: before, Existed: existed, After: after}
			if !existed {
				intent.Before = nil
			}
			raw, _ := json.Marshal(intent)
			if e := writeFileAtomicDurable(runtimeIntentPath(root), raw); e != nil {
				t.Fatal(e)
			}
			if published {
				if e := writeFileAtomicDurable(filepath.Join(root, "production.json"), after); e != nil {
					t.Fatal(e)
				}
			}
			// Simulate a fresh process: reconstruct solely from durable bytes.
			saved, e := os.ReadFile(runtimeIntentPath(root))
			if e != nil {
				t.Fatal(e)
			}
			var recovered nativeRuntimeIntent
			if e = runtimeJSON(saved, &recovered); e != nil {
				t.Fatal(e)
			}
			tx := runtimeDeploymentTransaction{root: root, before: recovered.Before, existed: recovered.Existed, after: recovered.After, published: true}
			if e = tx.Rollback(); e != nil {
				t.Fatal(e)
			}
			if e = sameRuntimeDeploymentPointer(root, recovered.Before, existed); e != nil {
				t.Fatal(e)
			}
		}
	}
}
func TestNativeRuntimeIntentCommitBoundToExactBytes(t *testing.T) {
	root := t.TempDir()
	after := []byte("test after")
	if e := writeFileAtomicDurable(filepath.Join(root, "production.json"), after); e != nil {
		t.Fatal(e)
	}
	u := nativeRuntimeUpdate{root: root, raw: []byte("test immutable intent"), tx: &runtimeDeploymentTransaction{root: root, after: after}}
	if e := u.Commit(); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(runtimeCommitPath(root))
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(b, []byte(runtimeDigest(u.raw))) {
		t.Fatal("commit not bound")
	}
	u.tx.after = []byte("different")
	if e = u.Commit(); e == nil {
		t.Fatal("commit ignored pointer drift")
	}
}
func TestNativeRuntimeEmptyHostSnapshot(t *testing.T) {
	s, e := snapshotManagedUpdateCaches(t.TempDir(), updateManifest{}, nil)
	if e != nil || len(s.profiles) != 0 {
		t.Fatalf("empty host snapshot: %+v %v", s, e)
	}
	// No vendor process is executed by the empty-profile snapshot.
}
func TestNativeRuntimeRecoveryRejectsEscapedSnapshot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if e := os.WriteFile(outside, []byte("bytes"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := validateRuntimeSaved(root, outside, runtimeDigest([]byte("bytes"))); e == nil {
		t.Fatal("escaped snapshot accepted")
	}
}
func fullNativeRecoveryFixture(t *testing.T) (string, string, string, nativeRuntimeIntent) {
	t.Helper()
	base := t.TempDir()
	home := filepath.Join(base, "air-worker")
	root := filepath.Join(home, "runtime")
	t.Setenv("LOCALAPPDATA", base)
	t.Setenv("AIR_WORKER_HOME", home)
	put := func(path string, b []byte) {
		t.Helper()
		if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(path, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	p, m, priv, key, current, previous := runtimePacketFixture(t)
	consumer := []byte("SYNTHETIC TEST ONLY consumer")
	p.ConsumerSHA256 = runtimeDigest(consumer)
	put(filepath.Join(base, "air-curator", "bin", "AirCuratorLearning.psm1"), consumer)
	newTray, oldTray := []byte("SYNTHETIC new tray"), []byte("SYNTHETIC old tray")
	m.Tray.SHA256 = runtimeDigest(newTray)
	m.Tray.Size = int64(len(newTray))
	m.ManifestSig = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, updateSignatureMessage(m)))
	packet := signRuntimePacketTest(t, p, priv)
	if e := os.MkdirAll(root, 0700); e != nil {
		t.Fatal(e)
	}
	plan, verify, e := runtimeReleasePlan(packet, m, p.ConsumerSHA256, current, previous, key)
	if e != nil {
		t.Fatal(e)
	}
	tx, e := prepareRuntimeDeployment(root, plan, verify)
	if e != nil {
		t.Fatal(e)
	}
	if e = tx.Publish(); e != nil {
		t.Fatal(e)
	}
	put(filepath.Join(home, "bin", "air-worker.exe"), current)
	put(filepath.Join(home, "bin", "air-worker-tray.exe"), newTray)
	intent := nativeRuntimeIntent{Schema: "air-worker.runtime-update-intent/v1", SnapshotID: "fixture-20261114", Manifest: m, Packet: packet, Before: tx.before, Existed: tx.existed, After: tx.after, OldCLI: runtimeDigest(previous), OldTray: runtimeDigest(oldTray)}
	snapshot := filepath.Join(home, "update", "rollback", intent.SnapshotID)
	put(filepath.Join(snapshot, "air-worker.exe"), previous)
	put(filepath.Join(snapshot, "air-worker-tray.exe"), oldTray)
	raw, _ := json.Marshal(intent)
	if e = writeFileAtomicDurable(runtimeIntentPath(root), raw); e != nil {
		t.Fatal(e)
	}
	return home, root, key, intent
}
func recoveryFixtureBytes(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	e := filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		out[rel] = runtimeDigest(b)
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func TestNativeRuntimeFullRecoveryRejectsForeignStateWithoutWrites(t *testing.T) {
	for _, kind := range []string{"pointer", "cli", "tray", "committed-cli", "committed-tray", "consumer", "current-slot", "previous-slot", "acceptance", "cache-drift"} {
		t.Run(kind, func(t *testing.T) {
			home, root, key, _ := fullNativeRecoveryFixture(t)
			committed := kind != "pointer" && kind != "cli" && kind != "tray"
			if committed {
				raw, e := os.ReadFile(runtimeIntentPath(root))
				if e != nil {
					t.Fatal(e)
				}
				if e = writeFileAtomicDurable(runtimeCommitPath(root), []byte(runtimeDigest(raw))); e != nil {
					t.Fatal(e)
				}
			}
			var target string
			switch kind {
			case "pointer":
				target = filepath.Join(root, "production.json")
			case "cli", "committed-cli":
				target = filepath.Join(home, "bin", "air-worker.exe")
			case "tray", "committed-tray":
				target = filepath.Join(home, "bin", "air-worker-tray.exe")
			case "consumer":
				target = filepath.Join(filepath.Dir(home), "air-curator", "bin", "AirCuratorLearning.psm1")
			case "current-slot":
				target = filepath.Join(root, "releases", "new", "air-worker.exe")
			case "previous-slot":
				target = filepath.Join(root, "releases", "old", "air-worker.exe")
			case "acceptance":
				target = filepath.Join(root, "releases", "old", "acceptance.json")
			}
			if target != "" {
				if e := os.WriteFile(target, []byte("foreign bytes"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			profiles := func() []distributionProfile { return nil }
			if kind == "cache-drift" {
				profiles = func() []distributionProfile { return []distributionProfile{{Host: "codex", ConfigDir: "foreign"}} }
			}
			before := recoveryFixtureBytes(t, filepath.Dir(home))
			recovered, e := recoverNativeRuntimeUpdateWithTrust(home, key, profiles)
			if e == nil || recovered {
				t.Fatalf("foreign state accepted: recovered=%v err=%v", recovered, e)
			}
			after := recoveryFixtureBytes(t, filepath.Dir(home))
			if !reflect.DeepEqual(before, after) {
				t.Fatal("recovery wrote files before refusing foreign state")
			}
		})
	}
}
func TestNativeRuntimeFullCommittedRecoveryValidatesThenClearsIntent(t *testing.T) {
	home, root, key, _ := fullNativeRecoveryFixture(t)
	raw, e := os.ReadFile(runtimeIntentPath(root))
	if e != nil {
		t.Fatal(e)
	}
	if e = writeFileAtomicDurable(runtimeCommitPath(root), []byte(runtimeDigest(raw))); e != nil {
		t.Fatal(e)
	}
	before := recoveryFixtureBytes(t, filepath.Dir(home))
	recovered, e := recoverNativeRuntimeUpdateWithTrust(home, key, func() []distributionProfile { return nil })
	if e != nil || recovered {
		t.Fatalf("committed cleanup: recovered=%v err=%v", recovered, e)
	}
	delete(before, filepath.Join("air-worker", "runtime", "update-intent.json"))
	after := recoveryFixtureBytes(t, filepath.Dir(home))
	if !reflect.DeepEqual(before, after) {
		t.Fatal("committed cleanup changed more than its intent")
	}
}
func TestNativeRuntimeCommittedCacheIdentity(t *testing.T) {
	_, m, _, _, _, _ := runtimePacketFixture(t)
	records := []runtimeCacheProfile{{Host: "codex", ConfigDir: filepath.Join(t.TempDir(), "codex")}}
	profile := distributionProfile{Host: "codex", ConfigDir: records[0].ConfigDir, HasState: true, PluginEnabled: true, Canonical: true, Version: m.Version, Revision: m.VCSRevision, PayloadSHA256: m.PayloadSHA256, CacheBinary: distributionBinaryIdentity{Readable: true, SHA256: m.CLI.SHA256}}
	if e := validateRuntimeCommittedCaches(records, []distributionProfile{profile}, m); e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"missing", "sha", "payload", "source", "disabled", "foreign"} {
		bad := profile
		switch kind {
		case "sha":
			bad.CacheBinary.SHA256 = "bad"
		case "payload":
			bad.PayloadSHA256 = "bad"
		case "source":
			bad.Revision = "bad"
		case "disabled":
			bad.PluginEnabled = false
		case "foreign":
			bad.ConfigDir = filepath.Join(t.TempDir(), "foreign")
		}
		actual := []distributionProfile{bad}
		if kind == "missing" {
			actual = nil
		}
		if e := validateRuntimeCommittedCaches(records, actual, m); e == nil {
			t.Fatalf("accepted %s cache drift", kind)
		}
	}
}
func TestNativeRuntimeManagerIndependentSync(t *testing.T) {
	root := t.TempDir()
	t.Setenv("USERPROFILE", root)
	t.Setenv("HOME", root)
	t.Setenv("LOCALAPPDATA", root)
	t.Setenv("AIR_WORKER_HOME", filepath.Join(root, "air-worker"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
	t.Setenv("PATH", filepath.Join(root, "no-executables"))
	if e := validatePinnedUpdateProfiles(nil, nil); e != nil {
		t.Fatal(e)
	}
	if e := validatePinnedUpdateProfiles(nil, []distributionProfile{{Host: "codex"}}); e == nil {
		t.Fatal("new profile silently adopted")
	}
	if e := validatePinnedUpdateProfiles([]distributionProfile{{Host: "codex"}}, nil); e == nil {
		t.Fatal("existing profile silently lost")
	}
	got, e := syncKnownPluginCaches(updateManifest{}, nil)
	if e != nil || len(got) != 0 {
		t.Fatalf("empty native installation required a manager: %v %v", got, e)
	}
}
func TestNativeRuntimePairRollbackGuardPreservesForeignBytes(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	stage := filepath.Join(root, "stage")
	snapshot := filepath.Join(root, "snapshot")
	for _, dir := range []string{filepath.Join(home, "bin"), stage, snapshot} {
		if e := os.MkdirAll(dir, 0700); e != nil {
			t.Fatal(e)
		}
	}
	oldCLI, oldTray := []byte("old cli"), []byte("old tray")
	currentCLI, currentTray := []byte("new cli"), []byte("new tray")
	dstCLI, dstTray := filepath.Join(home, "bin", "air-worker.exe"), filepath.Join(home, "bin", "air-worker-tray.exe")
	stagedCLI, stagedTray := filepath.Join(stage, "air-worker.exe"), filepath.Join(stage, "air-worker-tray.exe")
	for path, b := range map[string][]byte{
		dstCLI: oldCLI, dstTray: oldTray, stagedCLI: currentCLI, stagedTray: currentTray,
		filepath.Join(snapshot, "air-worker.exe"): oldCLI, filepath.Join(snapshot, "air-worker-tray.exe"): oldTray,
	} {
		if e := os.WriteFile(path, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	m := updateManifest{CLI: artifactForFile(t, stagedCLI, "air-worker.exe"), Tray: artifactForFile(t, stagedTray, "air-worker-tray.exe")}
	foreign := []byte("foreign replacement")
	guardCalled := false
	e := replaceUpdatePairTransactional(snapshot, home, stagedCLI, stagedTray, m, func(string) error {
		if e := os.WriteFile(dstCLI, foreign, 0600); e != nil {
			return e
		}
		return errors.New("simulated acceptance failure after external replacement")
	}, func() error { guardCalled = true; return errors.New("foreign runtime state") })
	if e == nil || !guardCalled {
		t.Fatal("rollback guard not enforced")
	}
	cli, _ := os.ReadFile(dstCLI)
	tray, _ := os.ReadFile(dstTray)
	if !bytes.Equal(cli, foreign) || !bytes.Equal(tray, currentTray) {
		t.Fatal("rollback restored native pair over foreign state")
	}
}

func TestNativeRuntimeRootRejectsJunctionBeforeMkdir(t *testing.T) {
	base := t.TempDir()
	external := filepath.Join(base, "external")
	owner := filepath.Join(base, "air-worker")
	if e := os.Mkdir(external, 0700); e != nil {
		t.Fatal(e)
	}
	out, e := exec.Command("cmd.exe", "/c", "mklink", "/J", owner, external).CombinedOutput()
	if e != nil {
		t.Skipf("junction creation unavailable without permission changes: %v %s", e, out)
	}
	defer os.Remove(owner)
	root := filepath.Join(owner, "runtime")
	release, e := createAndHoldNativeRuntimeRoot(root)
	if e == nil {
		release()
		t.Fatal("junction owner accepted")
	}
	entries, e := os.ReadDir(external)
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) != 0 {
		t.Fatalf("unchecked ancestor mutated external target: %v", entries)
	}
}
func TestNativeRuntimeRootCreatesOnlyGuardedLeaf(t *testing.T) {
	base := t.TempDir()
	owner := filepath.Join(base, "air-worker")
	if e := os.Mkdir(owner, 0700); e != nil {
		t.Fatal(e)
	}
	root := filepath.Join(owner, "runtime")
	release, e := createAndHoldNativeRuntimeRoot(root)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Rename(owner, owner+"-moved"); e == nil {
		release()
		t.Fatal("owner could be renamed during runtime lifetime")
	}
	release()
	if e = os.Rename(owner, owner+"-moved"); e != nil {
		t.Fatal(e)
	}
	absent := filepath.Join(base, "missing-owner", "runtime")
	if release, e = createAndHoldNativeRuntimeRoot(absent); e == nil {
		release()
		t.Fatal("missing owner silently created")
	}
	if _, e = os.Stat(filepath.Dir(absent)); !os.IsNotExist(e) {
		t.Fatalf("missing owner changed: %v", e)
	}
}
