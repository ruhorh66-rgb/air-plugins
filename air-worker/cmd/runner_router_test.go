package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCriterion64RouterRunnerBoundary(t *testing.T) {
	cfg := runConfig{Runners: map[string]runnerSpec{
		"router": {Kind: "router", Model: "air-auto"},
	}}
	r := resolveRunner(cfg, "router")
	if r.Kind != "router" || r.Model != "air-auto" {
		t.Fatalf("router rung resolved incorrectly: %+v", r)
	}
	paths := []string{"runner_router.go", filepath.Join("..", "tools", "router_stream_bridge.py")}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, forbidden := range []string{"OPENROUTER_API_KEY", "AIR_ROUTER_OPENROUTER_KEY", ":free\""} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("AirWorker transport must not own provider/model secret policy: %s contains %q", path, forbidden)
			}
		}
	}
}

func TestRouterSystemProxyParsing(t *testing.T) {
	cases := []struct{ in, http, https string }{
		{"127.0.0.1:10809", "http://127.0.0.1:10809", "http://127.0.0.1:10809"},
		{"http=proxy.local:8080;https=secure.local:8443", "http://proxy.local:8080", "http://secure.local:8443"},
		{"https=http://secure.local:8443", "http://secure.local:8443", "http://secure.local:8443"},
	}
	for _, tc := range cases {
		httpProxy, httpsProxy := parseSystemProxy(tc.in)
		if httpProxy != tc.http || httpsProxy != tc.https {
			t.Fatalf("parseSystemProxy(%q)=(%q,%q), want (%q,%q)", tc.in, httpProxy, httpsProxy, tc.http, tc.https)
		}
	}
}

func TestCriterion65RouterCodingAgentContract(t *testing.T) {
	if os.Getenv("AIR_ROUTER_LIVE") != "1" {
		t.Skip("set AIR_ROUTER_LIVE=1 for live free-model coding acceptance")
	}
	openCode := os.Getenv("AIR_OPENCODE_EXE")
	if openCode == "" {
		t.Fatal("AIR_OPENCODE_EXE is required for live acceptance")
	}
	if _, err := os.Stat(openCode); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	bridge, err := os.ReadFile(filepath.Join("..", "tools", "router_stream_bridge.py"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tools", "router_stream_bridge.py"), bridge, 0o644); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "fixture.txt")
	if err := os.WriteFile(fixture, []byte("BEFORE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	verify := `$raw=(Get-Content -LiteralPath (Join-Path $PSScriptRoot 'fixture.txt') -Raw).Trim()
if($raw -ne 'AFTER'){ Write-Error "fixture=$raw"; exit 7 }
Write-Output 'VERIFY_OK'
exit 0
`
	if err := os.WriteFile(filepath.Join(root, "verify.ps1"), []byte(verify), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_SHIM_BIN", filepath.Join(root, "missing-codex.cmd"))
	t.Setenv("ANTHROPIC_API_KEY", "")
	ctx := &loopCtx{Root: root, MaxTurns: 20}
	prompt := "Use tools. Read fixture.txt. Replace BEFORE with AFTER in fixture.txt. " +
		"Then run powershell -NoProfile -ExecutionPolicy Bypass -File verify.ps1. " +
		"Finish only after VERIFY_OK with exit 0."
	res := ctx.invokeRouter(openCode, prompt, runnerSpec{Kind: "router", Model: "air-auto"}, "84-live")
	if !res.Ok {
		t.Fatalf("router live smoke failed: subtype=%s detail=%s", res.Subtype, res.Detail)
	}
	got, err := os.ReadFile(fixture)
	if err != nil || strings.TrimSpace(string(got)) != "AFTER" {
		t.Fatalf("fixture was not edited: %v %q", err, got)
	}
	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(root, "verify.ps1"))
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "VERIFY_OK") {
		t.Fatalf("external verifier failed: %v %s", err, out)
	}
	receiptPath, _ := jobReceiptPaths(legacyScope(root), "84-live", "executor-router")
	receipt, err := readJobReceipt(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Provider != "openrouter" || !strings.HasSuffix(receipt.Model, ":free") {
		t.Fatalf("live acceptance did not prove OpenRouter free route: %+v", receipt)
	}
	if receipt.Cost == nil || *receipt.Cost != 0 {
		t.Fatalf("free route cost is not zero: %+v", receipt.Cost)
	}
	t.Logf("LIVE_ACCEPTANCE provider=%s model=%s cost=%.4f fixture=AFTER verifier=VERIFY_OK", receipt.Provider, receipt.Model, *receipt.Cost)
}

func TestCriterion66RouterExecutorCodexJudge(t *testing.T) {
	root := t.TempDir()
	scope := legacyScope(root)
	receiptPath, outputPath := jobReceiptPaths(scope, "84", "executor-router")
	if err := os.MkdirAll(filepath.Dir(receiptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	r, lock, err := startJobReceipt(scope, "84", "executor-router", receiptPath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.release()
	zero := 0.0
	r.Runner = "router"
	r.Provider = "openrouter"
	r.Model = "example/free:free"
	r.Cost = &zero
	if err := writeJobReceipt(receiptPath, r); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outputPath, []byte("router fixture output"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := finishJobReceipt(receiptPath, jobStatusDone); err != nil {
		t.Fatal(err)
	}
	got, err := readJobReceipt(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.Runner != "router" || got.Provider != "openrouter" || got.Model == "" || got.Cost == nil {
		t.Fatalf("router route evidence did not survive durable receipt: %+v", got)
	}
	data, err := os.ReadFile("runner_router.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(data)), "invokecodex") {
		t.Fatal("router executor must not invoke Codex; Codex remains external semantic judge")
	}
}

func TestRouterTreeHelper(t *testing.T) {
	mode := os.Getenv("AW_ROUTER_TREE_HELPER")
	if mode == "" {
		return
	}
	if mode == "parent" {
		exe, err := os.Executable()
		if err != nil {
			os.Exit(2)
		}
		child := exec.Command(exe, "-test.run=^TestRouterTreeHelper$")
		child.Env = append(os.Environ(), "AW_ROUTER_TREE_HELPER=child")
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
	} else if mode == "child" {
		if err := os.WriteFile(os.Getenv("AW_ROUTER_TREE_PID"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			os.Exit(2)
		}
	} else {
		os.Exit(2)
	}
	time.Sleep(30 * time.Second)
}

func TestRouterStopProcessKillsOwnedTree(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("process handle waiting is tested on Windows")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "child.pid")
	cmd := exec.Command(exe, "-test.run=^TestRouterTreeHelper$")
	cmd.Env = append(os.Environ(), "AW_ROUTER_TREE_HELPER=parent", "AW_ROUTER_TREE_PID="+pidPath)
	process, err := startLogged(cmd, filepath.Join(dir, "helper.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	parent, err := os.FindProcess(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Release()
	var child *os.Process
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(pidPath); err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
			if err != nil {
				t.Fatal(err)
			}
			child, err = os.FindProcess(pid)
			if err != nil {
				t.Fatal(err)
			}
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if child == nil {
		t.Fatal("owned descendant did not publish its PID")
	}
	t.Cleanup(func() { _ = child.Kill(); _ = child.Release() })
	stopped := make(chan struct{})
	go func() { _ = stopProcess(process); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("stopProcess did not return")
	}
	for name, process := range map[string]*os.Process{"parent": parent, "child": child} {
		done := make(chan error, 1)
		go func() { _, err := process.Wait(); done <- err }()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("waiting for %s: %v", name, err)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("owned %s survived cleanup", name)
		}
	}
}

func TestRouterMetadataWriteFailureStopsLaunch(t *testing.T) {
	root := t.TempDir()
	oldWriter := writeJobReceiptFunc
	t.Cleanup(func() { writeJobReceiptFunc = oldWriter })
	metadataAttempted := false
	writeJobReceiptFunc = func(path string, receipt *jobReceipt) error {
		if receipt.Runner == "router" && receipt.Status == jobStatusRunning {
			metadataAttempted = true
			return errors.New("router metadata write failed")
		}
		return oldWriter(path, receipt)
	}
	result := (&loopCtx{Root: root}).invokeRouter("missing-opencode", "test", runnerSpec{Kind: "router"}, "metadata-failure")
	if !metadataAttempted || result.Ok || result.Subtype != "runner_error" || !strings.Contains(result.Detail, "router metadata write failed") {
		t.Fatalf("metadata failure did not stop launch: attempted=%v result=%+v", metadataAttempted, result)
	}
}
