//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestInitSharedLearningWindowsRuntimeAliasCannotBypassOwnership(t *testing.T) {
	base := t.TempDir()
	productOne := filepath.Join(base, "product-one")
	productTwo := filepath.Join(base, "product-two")
	for _, product := range []string{productOne, productTwo} {
		if err := os.MkdirAll(product, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeLegacyOperationalRule(t, productOne, "LP-alias-winner", "alias-winner", "operational-procedure-v1")
	writeLegacyOperationalRule(t, productTwo, "LP-alias-loser", "alias-loser", "operational-procedure-v1")
	runtimeRoot := filepath.Join(base, "runtime-alias")
	runtimeAlias := runtimeRoot + "."

	originalFault := sharedLearningBootstrapFault
	defer func() { sharedLearningBootstrapFault = originalFault }()
	entered := make(chan struct{})
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	var once sync.Once
	sharedLearningBootstrapFault = func(phase string) error {
		if phase == "after-selector" {
			once.Do(func() {
				close(entered)
				<-release
			})
		}
		return nil
	}

	type bootstrapResult struct {
		report sharedLearningInitReport
		err    error
	}
	first := make(chan bootstrapResult, 1)
	go func() {
		report, err := initSharedLearning(productOne, runtimeRoot, "air-worker-alias-winner")
		first <- bootstrapResult{report: report, err: err}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("winner did not publish selector")
	}

	winnerInfo, err := os.Stat(runtimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	aliasInfo, err := os.Stat(runtimeAlias)
	if err != nil {
		t.Fatalf("Win32 alias did not resolve to winner runtime: %v", err)
	}
	if !os.SameFile(winnerInfo, aliasInfo) {
		t.Fatal("fixture paths are not aliases of the same Windows directory")
	}

	intentBefore, found, err := readSharedLearningBootstrapIntent(runtimeRoot)
	if err != nil || !found || intentBefore.ProductID != "air-worker-alias-winner" || intentBefore.Status != "ready" {
		t.Fatalf("winner intent not ready: %+v found=%v err=%v", intentBefore, found, err)
	}

	second := make(chan bootstrapResult, 1)
	go func() {
		report, err := initSharedLearning(productTwo, runtimeAlias, "air-worker-alias-loser")
		second <- bootstrapResult{report: report, err: err}
	}()
	var loser bootstrapResult
	select {
	case loser = <-second:
	case <-time.After(3 * time.Second):
		t.Fatal("aliased loser did not fail closed before bootstrap I/O")
	}
	if loser.err == nil {
		t.Fatalf("aliased loser unexpectedly succeeded: %+v", loser.report)
	}
	intentDuring, found, err := readSharedLearningBootstrapIntent(runtimeRoot)
	if err != nil || !found || intentDuring.ProductID != intentBefore.ProductID || intentDuring.Status != "ready" {
		t.Fatalf("aliased loser changed winner intent: before=%+v during=%+v found=%v err=%v", intentBefore, intentDuring, found, err)
	}
	if _, err := os.Stat(filepath.Join(productTwo, sharedLearningConfigFile)); !os.IsNotExist(err) {
		t.Fatalf("aliased loser published selector: %v", err)
	}
	if _, err := os.Stat(filepath.Join(productTwo, ".air-learning-owner.json")); !os.IsNotExist(err) {
		t.Fatalf("aliased loser changed owner binding: %v", err)
	}

	close(release)
	released = true
	winner := <-first
	if winner.err != nil || winner.report.Status != "initialized" {
		t.Fatalf("winner failed after alias rejection: %+v %v", winner.report, winner.err)
	}
	finalIntent, found, err := readSharedLearningBootstrapIntent(runtimeRoot)
	if err != nil || !found || finalIntent.ProductID != "air-worker-alias-winner" || finalIntent.Status != "complete" {
		t.Fatalf("winner state was not preserved: %+v found=%v err=%v", finalIntent, found, err)
	}
}

func TestInitSharedLearningRejectsRawWindowsAliasSpellingsBeforeIO(t *testing.T) {
	base := t.TempDir()
	runtimeRoot := filepath.Join(base, "runtime-raw-alias")
	existing, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	forwardExisting := strings.ReplaceAll(existing, `\`, "/")
	cases := []struct {
		name string
		path string
		want string
	}{
		{name: "trailing-space", path: runtimeRoot + " ", want: "trailing-dot/space"},
		{name: "win32-device", path: `\\?\` + existing, want: "device-path spelling"},
		{name: "win32-device-forward", path: `//?/` + forwardExisting, want: "device-path spelling"},
		{name: "win32-device-mixed", path: `\\?/` + forwardExisting, want: "device-path spelling"},
		{name: "win32-dot-device", path: `\\.\` + existing, want: "device-path spelling"},
		{name: "win32-dot-device-forward", path: `//./` + forwardExisting, want: "device-path spelling"},
		{name: "nt-device", path: `\??\` + existing, want: "device-path spelling"},
		{name: "nt-device-forward", path: `/??/` + forwardExisting, want: "device-path spelling"},
		{name: "nt-device-double-forward", path: `//??/` + forwardExisting, want: "device-path spelling"},
		{name: "nt-device-mixed", path: `\??/` + forwardExisting, want: "device-path spelling"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			product := filepath.Join(base, "product-"+tc.name)
			if err := os.MkdirAll(product, 0o755); err != nil {
				t.Fatal(err)
			}
			writeLegacyOperationalRule(t, product, "LP-"+tc.name, tc.name, "operational-procedure-v1")

			report, initErr := initSharedLearning(product, tc.path, "air-worker-"+tc.name)
			if initErr == nil {
				t.Fatalf("raw alias unexpectedly initialized: %+v", report)
			}
			if !strings.Contains(initErr.Error(), tc.want) {
				t.Fatalf("raw alias reached wrong rejection path: %v; want %q", initErr, tc.want)
			}
			if _, err := os.Stat(filepath.Join(product, sharedLearningConfigFile)); !os.IsNotExist(err) {
				t.Fatalf("raw alias published selector: %v", err)
			}
			if _, err := os.Stat(filepath.Join(product, ".air-learning-owner.json")); !os.IsNotExist(err) {
				t.Fatalf("raw alias created owner binding: %v", err)
			}
			if _, err := os.Stat(runtimeRoot); !os.IsNotExist(err) {
				t.Fatalf("raw alias created canonical runtime: %v", err)
			}
			if _, err := os.Stat(sharedLearningBootstrapIntentPath(runtimeRoot)); !os.IsNotExist(err) {
				t.Fatalf("raw alias created bootstrap intent: %v", err)
			}
		})
	}
}
