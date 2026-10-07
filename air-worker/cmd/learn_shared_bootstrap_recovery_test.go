package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ruhorh66-rgb/air-modules/learning"
)

func TestInitSharedLearningResumesAfterSelectorInterruption(t *testing.T) {
	product := filepath.Join(t.TempDir(), "product")
	if err := os.MkdirAll(product, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLegacyOperationalRule(t, product, "LP-crash-selector", "selector-resume", "operational-procedure-v1")
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")

	originalFault := sharedLearningBootstrapFault
	defer func() { sharedLearningBootstrapFault = originalFault }()
	fired := false
	sharedLearningBootstrapFault = func(phase string) error {
		if phase == "after-selector" && !fired {
			fired = true
			return errors.New("fixture crash after selector")
		}
		return nil
	}

	if _, err := initSharedLearning(product, runtimeRoot, "air-worker-test"); err == nil {
		t.Fatal("selector interruption was not surfaced")
	}
	if _, err := os.Stat(filepath.Join(product, sharedLearningConfigFile)); err != nil {
		t.Fatalf("durable selector missing after interruption: %v", err)
	}
	if _, err := os.Stat(filepath.Join(product, "learn", "rules", "LP-crash-selector.json")); err != nil {
		t.Fatalf("legacy source was moved before crash recovery completed: %v", err)
	}
	if _, on, err := readSharedLearningSettings(product); err != nil || !on {
		t.Fatalf("verified bootstrap overlap did not keep shared mode readable: on=%v err=%v", on, err)
	}

	sharedLearningBootstrapFault = func(string) error { return nil }
	report, err := initSharedLearning(product, runtimeRoot, "air-worker-test")
	if err != nil || report.Status != "recovered" {
		t.Fatalf("bootstrap did not resume: %+v %v", report, err)
	}
	if _, err := os.Stat(filepath.Join(product, "learn", "rules")); !os.IsNotExist(err) {
		t.Fatalf("legacy active directory survived recovery: %v", err)
	}
	if _, err := os.Stat(filepath.Join(product, "learn", "legacy-rules", "LP-crash-selector.json")); err != nil {
		t.Fatalf("legacy provenance archive missing: %v", err)
	}
	intent, found, err := readSharedLearningBootstrapIntent(runtimeRoot)
	if err != nil || !found || intent.Status != "complete" {
		t.Fatalf("bootstrap intent not complete after resume: %+v found=%v err=%v", intent, found, err)
	}
}

func TestInitSharedLearningRecoversFailureAfterTargetWrite(t *testing.T) {
	product := filepath.Join(t.TempDir(), "product")
	if err := os.MkdirAll(product, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLegacyOperationalRule(t, product, "LP-post-target", "post-target-recovery", "operational-procedure-v1")
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")

	originalExecute := sharedLearningBootstrapExecute
	defer func() { sharedLearningBootstrapExecute = originalExecute }()
	injected := false
	sharedLearningBootstrapExecute = func(root string, s sharedLearningSettings, op string, data any) (learning.Response, error) {
		res, err := originalExecute(root, s, op, data)
		if err != nil || op != "propose" || injected {
			return res, err
		}
		injected = true
		ledgerPath := filepath.Join(s.RuntimeRoot, "ledger.jsonl")
		b, readErr := os.ReadFile(ledgerPath)
		if readErr != nil {
			return res, readErr
		}
		lines := strings.Split(strings.TrimSpace(string(b)), "\n")
		if len(lines) == 0 || strings.TrimSpace(lines[len(lines)-1]) == "" {
			return res, errors.New("fixture ledger record missing")
		}
		var ledger map[string]any
		if json.Unmarshal([]byte(lines[len(lines)-1]), &ledger) != nil {
			return res, errors.New("fixture ledger decode failed")
		}
		prefix := strings.Join(lines[:len(lines)-1], "\n")
		if prefix != "" {
			prefix += "\n"
		}
		if err := os.WriteFile(ledgerPath, []byte(prefix), 0o600); err != nil {
			return res, err
		}
		tx, _ := ledger["transaction_id"].(string)
		if tx == "" {
			return res, errors.New("fixture transaction id missing")
		}
		if err := os.MkdirAll(filepath.Join(s.RuntimeRoot, "transactions"), 0o700); err != nil {
			return res, err
		}
		wal := map[string]any{"schema": "air.learning.transaction/v1", "product_id": s.ProductID, "ledger": ledger}
		raw, _ := json.MarshalIndent(wal, "", "  ")
		if err := os.WriteFile(filepath.Join(s.RuntimeRoot, "transactions", tx+".json"), append(raw, '\n'), 0o600); err != nil {
			return res, err
		}
		return res, errors.New("fixture failure after target write")
	}

	if _, err := initSharedLearning(product, runtimeRoot, "air-worker-test"); err == nil {
		t.Fatal("post-target injected failure was not surfaced")
	}
	if _, err := os.Stat(filepath.Join(product, sharedLearningConfigFile)); !os.IsNotExist(err) {
		t.Fatal("selector was published despite pre-cutover migration failure")
	}
	if _, err := os.Stat(filepath.Join(product, "learn", "rules", "LP-post-target.json")); err != nil {
		t.Fatalf("legacy active rule was lost: %v", err)
	}
	intent, found, err := readSharedLearningBootstrapIntent(runtimeRoot)
	if err != nil || !found || len(intent.Rules) != 1 {
		t.Fatalf("recovery intent missing: %+v found=%v err=%v", intent, found, err)
	}
	target := filepath.Join(product, filepath.FromSlash(intent.Rules[0].Target))
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("module recovery did not restore pre-mutation target: %v", err)
	}
	if matches, _ := filepath.Glob(filepath.Join(runtimeRoot, "transactions", "TX-*.json")); len(matches) != 0 {
		t.Fatalf("module WAL survived status recovery: %v", matches)
	}

	sharedLearningBootstrapExecute = originalExecute
	report, err := initSharedLearning(product, runtimeRoot, "air-worker-test")
	if err != nil || report.Status != "initialized" {
		t.Fatalf("bootstrap retry did not finish after WAL recovery: %+v %v", report, err)
	}
}

func TestInitSharedLearningConcurrentInitializersPreserveWinner(t *testing.T) {
	product := filepath.Join(t.TempDir(), "product")
	if err := os.MkdirAll(product, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLegacyOperationalRule(t, product, "LP-concurrent", "concurrent-bootstrap", "operational-procedure-v1")
	runtimeOne := filepath.Join(t.TempDir(), "runtime-one")
	runtimeTwo := filepath.Join(t.TempDir(), "runtime-two")

	originalFault := sharedLearningBootstrapFault
	defer func() { sharedLearningBootstrapFault = originalFault }()
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	sharedLearningBootstrapFault = func(phase string) error {
		if phase == "after-intent" {
			once.Do(func() {
				close(entered)
				<-release
			})
		}
		return nil
	}

	type result struct {
		report sharedLearningInitReport
		err    error
	}
	first := make(chan result, 1)
	second := make(chan result, 1)
	go func() {
		report, err := initSharedLearning(product, runtimeOne, "air-worker-test")
		first <- result{report: report, err: err}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first initializer did not reach bootstrap lock scope")
	}
	go func() {
		report, err := initSharedLearning(product, runtimeTwo, "air-worker-test")
		second <- result{report: report, err: err}
	}()
	time.Sleep(100 * time.Millisecond)
	close(release)

	r1 := <-first
	r2 := <-second
	if r1.err != nil || r1.report.Status != "initialized" {
		t.Fatalf("winning initializer failed: %+v %v", r1.report, r1.err)
	}
	if r2.err == nil {
		t.Fatalf("losing initializer unexpectedly succeeded: %+v", r2.report)
	}
	s, on, err := readSharedLearningSettings(product)
	if err != nil || !on || !sameLearningPath(s.RuntimeRoot, runtimeOne) {
		t.Fatalf("winner selector was altered: %+v on=%v err=%v", s, on, err)
	}
	if _, err := os.Stat(sharedLearningBootstrapIntentPath(runtimeTwo)); !os.IsNotExist(err) {
		t.Fatalf("losing initializer mutated its runtime: %v", err)
	}
}

func TestInitSharedLearningAlreadyInitializedVerifiesMigration(t *testing.T) {
	product := filepath.Join(t.TempDir(), "product")
	if err := os.MkdirAll(product, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLegacyOperationalRule(t, product, "LP-verify-existing", "verify-existing", "operational-procedure-v1")
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	if _, err := initSharedLearning(product, runtimeRoot, "air-worker-test"); err != nil {
		t.Fatal(err)
	}
	intent, found, err := readSharedLearningBootstrapIntent(runtimeRoot)
	if err != nil || !found || len(intent.Rules) != 1 {
		t.Fatalf("intent missing: %+v %v", intent, err)
	}
	target := filepath.Join(product, filepath.FromSlash(intent.Rules[0].Target))
	if err := os.WriteFile(target, []byte("# corrupted\n\n## Procedure\nwrong\n\n## Pitfalls\nwrong\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if report, err := initSharedLearning(product, runtimeRoot, "air-worker-test"); err == nil {
		t.Fatalf("corrupted initialized migration accepted as %+v", report)
	}
}

func TestInitSharedLearningDistinctProductsSharedRuntimePreserveWinner(t *testing.T) {
	base := t.TempDir()
	productOne := filepath.Join(base, "product-one")
	productTwo := filepath.Join(base, "product-two")
	for _, product := range []string{productOne, productTwo} {
		if err := os.MkdirAll(product, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeLegacyOperationalRule(t, productOne, "LP-runtime-winner", "runtime-winner", "operational-procedure-v1")
	writeLegacyOperationalRule(t, productTwo, "LP-runtime-loser", "runtime-loser", "operational-procedure-v1")
	runtimeRoot := filepath.Join(base, "shared-runtime")

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
	second := make(chan bootstrapResult, 1)
	go func() {
		report, err := initSharedLearning(productOne, runtimeRoot, "air-worker-product-one")
		first <- bootstrapResult{report: report, err: err}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("winning initializer did not publish selector")
	}

	intentBefore, found, err := readSharedLearningBootstrapIntent(runtimeRoot)
	if err != nil || !found || intentBefore.ProductID != "air-worker-product-one" || intentBefore.Status != "ready" {
		t.Fatalf("winner intent not ready before loser: %+v found=%v err=%v", intentBefore, found, err)
	}
	if _, err := os.Stat(filepath.Join(productOne, sharedLearningConfigFile)); err != nil {
		t.Fatalf("winner selector not published: %v", err)
	}

	go func() {
		report, err := initSharedLearning(productTwo, runtimeRoot, "air-worker-product-two")
		second <- bootstrapResult{report: report, err: err}
	}()
	time.Sleep(150 * time.Millisecond)
	select {
	case r := <-second:
		t.Fatalf("loser escaped shared-runtime lock before winner completed: %+v %v", r.report, r.err)
	default:
	}
	intentDuring, found, err := readSharedLearningBootstrapIntent(runtimeRoot)
	if err != nil || !found || intentDuring.ProductID != intentBefore.ProductID || intentDuring.Status != "ready" {
		t.Fatalf("loser changed winner intent while runtime lock held: before=%+v during=%+v found=%v err=%v", intentBefore, intentDuring, found, err)
	}
	if _, err := os.Stat(filepath.Join(productTwo, sharedLearningConfigFile)); !os.IsNotExist(err) {
		t.Fatalf("loser published selector while winner held runtime lock: %v", err)
	}

	close(release)
	released = true
	r1 := <-first
	r2 := <-second
	if r1.err != nil || r1.report.Status != "initialized" {
		t.Fatalf("winning initializer failed: %+v %v", r1.report, r1.err)
	}
	if r2.err == nil {
		t.Fatalf("losing initializer unexpectedly succeeded: %+v", r2.report)
	}

	finalIntent, found, err := readSharedLearningBootstrapIntent(runtimeRoot)
	if err != nil || !found || finalIntent.ProductID != "air-worker-product-one" || finalIntent.Status != "complete" {
		t.Fatalf("winner intent was not preserved: %+v found=%v err=%v", finalIntent, found, err)
	}
	s, on, err := readSharedLearningSettings(productOne)
	if err != nil || !on || s.ProductID != "air-worker-product-one" || !sameLearningPath(s.RuntimeRoot, runtimeRoot) {
		t.Fatalf("winner selector was altered: %+v on=%v err=%v", s, on, err)
	}
	if _, err := os.Stat(filepath.Join(productTwo, sharedLearningConfigFile)); !os.IsNotExist(err) {
		t.Fatalf("loser left selector behind: %v", err)
	}
	if _, err := os.Stat(filepath.Join(productTwo, ".air-learning-owner.json")); !os.IsNotExist(err) {
		t.Fatalf("loser changed shared-learning owner binding: %v", err)
	}
}
