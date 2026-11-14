//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type runtimeCrashFixture struct {
	Home    string
	Root    string
	Key     string
	Intent  nativeRuntimeIntent
	Mode    string
	CrashAt string
}
type runtimeCrashResult struct {
	Recovered bool
	Error     string
}
type runtimeCrashMarker struct {
	Stage string
	PID   int
}

// Only explicitly launched copies of the test executable enter this helper.
// Every process action is inert; no production tray/vendor executable is used.
func TestNativeRuntimeCrashChild(t *testing.T) {
	configPath := os.Getenv("AW_RUNTIME_CRASH_FIXTURE")
	if configPath == "" {
		return
	}
	raw, e := os.ReadFile(configPath)
	if e != nil {
		t.Fatal(e)
	}
	var f runtimeCrashFixture
	if e = json.Unmarshal(raw, &f); e != nil {
		t.Fatal(e)
	}
	base := filepath.Dir(configPath)
	t.Setenv("LOCALAPPDATA", base)
	t.Setenv("AIR_WORKER_HOME", f.Home)
	t.Setenv("USERPROFILE", base)
	t.Setenv("HOME", base)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(base, "empty-claude"))
	t.Setenv("CODEX_HOME", filepath.Join(base, "empty-codex"))
	checkpoint := func(stage string) {
		if stage != f.CrashAt {
			return
		}
		b, _ := json.Marshal(runtimeCrashMarker{stage, os.Getpid()})
		if e := writeFileAtomicDurable(filepath.Join(base, "crash.marker"), b); e != nil {
			t.Fatal(e)
		}
		// Parent must terminate this process. No deferred cleanup is exercised.
		for {
			time.Sleep(time.Hour)
		}
	}
	record := func(s string) {
		file, e := os.OpenFile(filepath.Join(base, "actions.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = file.WriteString(s + "\n"); e != nil {
			file.Close()
			t.Fatal(e)
		}
		if e = file.Close(); e != nil {
			t.Fatal(e)
		}
	}
	var outcome runtimeCrashResult
	switch f.Mode {
	case "apply":
		currentPath := filepath.Join(f.Root, "releases", "new", "air-worker.exe")
		snapshot := filepath.Join(f.Home, "update", "rollback", f.Intent.SnapshotID)
		current, e := os.ReadFile(currentPath)
		if e != nil {
			t.Fatal(e)
		}
		previous, e := os.ReadFile(filepath.Join(snapshot, "air-worker.exe"))
		if e != nil {
			t.Fatal(e)
		}
		consumer, e := installedRuntimeConsumerSHA()
		if e != nil {
			t.Fatal(e)
		}
		plan, verify, e := runtimeReleasePlan(f.Intent.Packet, f.Intent.Manifest, consumer, current, previous, f.Key)
		if e != nil {
			t.Fatal(e)
		}
		release, e := createAndHoldNativeRuntimeRoot(f.Root)
		if e != nil {
			t.Fatal(e)
		}
		defer release()
		tx, e := prepareRuntimeDeployment(f.Root, plan, verify)
		if e != nil {
			t.Fatal(e)
		}
		u := nativeRuntimeUpdate{root: f.Root, tx: tx, intent: f.Intent}
		checkpoint("before-intent")
		if e = u.beginWithKey(snapshot, managedCacheSnapshot{}, f.Key); e != nil {
			t.Fatal(e)
		}
		checkpoint("after-intent")
		if e = copyFile(currentPath, filepath.Join(f.Home, "bin", "air-worker.exe")); e != nil {
			t.Fatal(e)
		}
		checkpoint("after-cli")
		if e = copyFile(filepath.Join(base, "new-tray.bin"), filepath.Join(f.Home, "bin", "air-worker-tray.exe")); e != nil {
			t.Fatal(e)
		}
		checkpoint("after-pair")
		if e = u.Publish(); e != nil {
			t.Fatal(e)
		}
		checkpoint("after-pointer")
		if e = u.Commit(); e != nil {
			t.Fatal(e)
		}
		checkpoint("after-commit")
		t.Fatal("apply helper reached end without its requested kill cutpoint")
	case "recover":
		recovered, e := recoverNativeRuntimeUpdateWithActions(f.Home, f.Key, func() []distributionProfile { return nil },
			nativeRuntimeRecoveryActions{
				stop:       func() (bool, bool) { record("stop"); return false, true },
				restart:    func(string) error { record("restart"); return nil },
				disabled:   func() bool { return false },
				version:    func(string) string { return "fixture-old" },
				checkpoint: checkpoint,
			})
		outcome.Recovered = recovered
		if e != nil {
			outcome.Error = e.Error()
		}
	default:
		t.Fatalf("unknown fixture mode %q", f.Mode)
	}
	b, _ := json.Marshal(outcome)
	if e = writeFileAtomicDurable(filepath.Join(base, "recovery-result.json"), b); e != nil {
		t.Fatal(e)
	}
}

func newNativeCrashFixture(t *testing.T) runtimeCrashFixture {
	t.Helper()
	home, root, key, intent := fullNativeRecoveryFixture(t)
	base := filepath.Dir(home)
	tray, e := os.ReadFile(filepath.Join(home, "bin", "air-worker-tray.exe"))
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(base, "new-tray.bin"), tray, 0600); e != nil {
		t.Fatal(e)
	}
	snapshot := filepath.Join(home, "update", "rollback", intent.SnapshotID)
	for _, name := range []string{"air-worker.exe", "air-worker-tray.exe"} {
		if e = copyFile(filepath.Join(snapshot, name), filepath.Join(home, "bin", name)); e != nil {
			t.Fatal(e)
		}
	}
	for _, path := range []string{filepath.Join(root, "production.json"), runtimeIntentPath(root)} {
		if e = os.Remove(path); e != nil {
			t.Fatal(e)
		}
	}
	return runtimeCrashFixture{Home: home, Root: root, Key: key, Intent: intent}
}

// A synthetic pre-existing accepted fixture, never a production receipt.
func nativeCrashWithPriorPointer(t *testing.T, f runtimeCrashFixture) runtimeCrashFixture {
	t.Helper()
	oldManifest, e := os.ReadFile(filepath.Join(f.Root, "releases", "old", "manifest.json"))
	if e != nil {
		t.Fatal(e)
	}
	var older runtimeManifest
	if e = runtimeJSON(oldManifest, &older); e != nil {
		t.Fatal(e)
	}
	older.ReleaseID = "older"
	olderDir := filepath.Join(f.Root, "releases", "older")
	if e = os.Mkdir(olderDir, 0700); e != nil {
		t.Fatal(e)
	}
	olderRaw, _ := json.Marshal(older)
	if e = os.WriteFile(filepath.Join(olderDir, "manifest.json"), olderRaw, 0600); e != nil {
		t.Fatal(e)
	}
	if e = copyFile(filepath.Join(f.Root, "releases", "old", older.Artifact.Path), filepath.Join(olderDir, older.Artifact.Path)); e != nil {
		t.Fatal(e)
	}
	before, _ := json.Marshal(runtimePointer{1, "air-worker", "old", runtimeDigest(oldManifest), true, "older"})
	f.Intent.Before = append(before, '\n')
	f.Intent.Existed = true
	if e = writeFileAtomicDurable(filepath.Join(f.Root, "production.json"), f.Intent.Before); e != nil {
		t.Fatal(e)
	}
	if _, e = resolveRuntime(f.Root, "air-curator", "legacy-learning", "air-worker.legacy-learning/v1"); e != nil {
		t.Fatal(e)
	}
	return f
}
func runNativeCrashChild(t *testing.T, f runtimeCrashFixture) runtimeCrashResult {
	t.Helper()
	base := filepath.Dir(f.Home)
	config := filepath.Join(base, "fixture.json")
	raw, _ := json.Marshal(f)
	if e := os.WriteFile(config, raw, 0600); e != nil {
		t.Fatal(e)
	}
	_ = os.Remove(filepath.Join(base, "crash.marker"))
	_ = os.Remove(filepath.Join(base, "recovery-result.json"))
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(exe, "-test.run=^TestNativeRuntimeCrashChild$", "-test.v")
	cmd.Env = append(os.Environ(), "AW_RUNTIME_CRASH_FIXTURE="+config)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	if f.CrashAt != "" {
		until := time.NewTimer(20 * time.Second)
		defer until.Stop()
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case e = <-done:
				t.Fatalf("child exited before %s: %v %s", f.CrashAt, e, output.String())
			case <-until.C:
				_ = cmd.Process.Kill()
				<-done
				t.Fatalf("child did not reach %s: %s", f.CrashAt, output.String())
			case <-tick.C:
				b, e := os.ReadFile(filepath.Join(base, "crash.marker"))
				if e != nil {
					continue
				}
				var marker runtimeCrashMarker
				if json.Unmarshal(b, &marker) != nil {
					continue
				}
				if marker.Stage != f.CrashAt || marker.PID != cmd.Process.Pid {
					t.Fatalf("wrong crash marker: %+v", marker)
				}
				if e = cmd.Process.Kill(); e != nil {
					t.Fatal(e)
				}
				select {
				case e = <-done:
				case <-time.After(10 * time.Second):
					t.Fatal("killed child did not exit")
				}
				if e == nil {
					t.Fatal("child exited normally instead of being killed")
				}
				t.Logf("terminated child PID=%d at durable cutpoint=%s", marker.PID, marker.Stage)
				return runtimeCrashResult{}
			}
		}
	}
	select {
	case e = <-done:
		if e != nil {
			t.Fatalf("fresh recovery child failed: %v %s", e, output.String())
		}
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		t.Fatalf("fresh recovery timed out: %s", output.String())
	}
	b, e := os.ReadFile(filepath.Join(base, "recovery-result.json"))
	if e != nil {
		t.Fatal(e)
	}
	var result runtimeCrashResult
	if e = json.Unmarshal(b, &result); e != nil {
		t.Fatal(e)
	}
	t.Logf("fresh recovery PID=%d recovered=%t error=%q", cmd.Process.Pid, result.Recovered, result.Error)
	return result
}
func assertNativeCrashConsistency(t *testing.T, f runtimeCrashFixture, wantNew bool) {
	t.Helper()
	cli, tray := f.Intent.OldCLI, f.Intent.OldTray
	if wantNew {
		cli, tray = f.Intent.Manifest.CLI.SHA256, f.Intent.Manifest.Tray.SHA256
	}
	for name, want := range map[string]string{"air-worker.exe": cli, "air-worker-tray.exe": tray} {
		got, e := sha256File(filepath.Join(f.Home, "bin", name))
		if e != nil || got != want {
			t.Fatalf("inconsistent %s: %s want %s error=%v", name, got, want, e)
		}
	}
	if wantNew {
		if e := sameRuntimeDeploymentPointer(f.Root, f.Intent.After, true); e != nil {
			t.Fatal(e)
		}
		if _, e := resolveRuntime(f.Root, "air-curator", "legacy-learning", "air-worker.legacy-learning/v1"); e != nil {
			t.Fatal(e)
		}
	} else {
		if e := sameRuntimeDeploymentPointer(f.Root, f.Intent.Before, f.Intent.Existed); e != nil {
			t.Fatal(e)
		}
		resolved, e := resolveRuntime(f.Root, "air-curator", "legacy-learning", "air-worker.legacy-learning/v1")
		if f.Intent.Existed {
			if e != nil || resolved.ReleaseID != "old" {
				t.Fatalf("prior accepted runtime not restored: %+v %v", resolved, e)
			}
		} else if e == nil {
			t.Fatal("bootstrap rollback fabricated an accepted runtime")
		}
	}
	if _, e := os.Stat(runtimeIntentPath(f.Root)); !os.IsNotExist(e) {
		t.Fatalf("recovery left intent: %v", e)
	}
}
func assertNativeCrashRepeatNoop(t *testing.T, f runtimeCrashFixture) {
	t.Helper()
	before := recoveryFixtureBytes(t, f.Home)
	actionsBefore, _ := os.ReadFile(filepath.Join(filepath.Dir(f.Home), "actions.log"))
	result := runNativeCrashChild(t, f)
	if result.Error != "" || result.Recovered {
		t.Fatalf("duplicate recovery was not a no-op: %+v", result)
	}
	after := recoveryFixtureBytes(t, f.Home)
	actionsAfter, _ := os.ReadFile(filepath.Join(filepath.Dir(f.Home), "actions.log"))
	if !reflect.DeepEqual(before, after) || !bytes.Equal(actionsBefore, actionsAfter) {
		t.Fatal("repeat recovery changed files or dispatched another action")
	}
}

func TestNativeRuntimeProcessKillApplyCutpoints(t *testing.T) {
	for _, prior := range []bool{false, true} {
		for _, stage := range []string{"before-intent", "after-intent", "after-cli", "after-pair", "after-pointer", "after-commit"} {
			t.Run(fmt.Sprintf("prior-%t/%s", prior, stage), func(t *testing.T) {
				f := newNativeCrashFixture(t)
				if prior {
					f = nativeCrashWithPriorPointer(t, f)
				}
				f.Mode = "apply"
				f.CrashAt = stage
				runNativeCrashChild(t, f)
				f.Mode = "recover"
				f.CrashAt = ""
				result := runNativeCrashChild(t, f)
				if result.Error != "" {
					t.Fatal(result.Error)
				}
				wantNew := stage == "after-commit"
				wantRecovered := stage != "before-intent" && !wantNew
				if result.Recovered != wantRecovered {
					t.Fatalf("recovered=%t want %t", result.Recovered, wantRecovered)
				}
				assertNativeCrashConsistency(t, f, wantNew)
				assertNativeCrashRepeatNoop(t, f)
			})
		}
	}
}
func TestNativeRuntimeProcessKillRecoveryCutpoints(t *testing.T) {
	for _, stage := range []string{"recovery-before-pointer", "recovery-after-pointer", "recovery-after-binaries", "recovery-after-state", "recovery-after-intent-clear"} {
		t.Run(stage, func(t *testing.T) {
			f := nativeCrashWithPriorPointer(t, newNativeCrashFixture(t))
			f.Mode = "apply"
			f.CrashAt = "after-pointer"
			runNativeCrashChild(t, f)
			f.Mode = "recover"
			f.CrashAt = stage
			runNativeCrashChild(t, f)
			f.CrashAt = ""
			result := runNativeCrashChild(t, f)
			if result.Error != "" {
				t.Fatal(result.Error)
			}
			assertNativeCrashConsistency(t, f, false)
			assertNativeCrashRepeatNoop(t, f)
		})
	}
}
func TestNativeRuntimeProcessKillForeignStateRefusal(t *testing.T) {
	for _, kind := range []string{"foreign-cli", "foreign-pointer", "torn-cli"} {
		t.Run(kind, func(t *testing.T) {
			f := newNativeCrashFixture(t)
			f.Mode = "apply"
			f.CrashAt = "after-pointer"
			runNativeCrashChild(t, f)
			target := filepath.Join(f.Home, "bin", "air-worker.exe")
			data := []byte("third-party executable")
			if kind == "foreign-pointer" {
				target = filepath.Join(f.Root, "production.json")
				data = []byte("third-party pointer")
			}
			if kind == "torn-cli" {
				data = []byte{}
			}
			if e := os.WriteFile(target, data, 0600); e != nil {
				t.Fatal(e)
			}
			before := recoveryFixtureBytes(t, f.Home)
			f.Mode = "recover"
			f.CrashAt = ""
			for i := 0; i < 2; i++ {
				result := runNativeCrashChild(t, f)
				if result.Error == "" || result.Recovered {
					t.Fatalf("foreign/unknown state accepted: %+v", result)
				}
				if !reflect.DeepEqual(before, recoveryFixtureBytes(t, f.Home)) {
					t.Fatal("refused recovery overwrote foreign state")
				}
				if b, _ := os.ReadFile(filepath.Join(filepath.Dir(f.Home), "actions.log")); len(b) != 0 {
					t.Fatalf("refused recovery dispatched process actions: %q", b)
				}
			}
		})
	}
}
