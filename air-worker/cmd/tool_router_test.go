package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCriterion70RouterToolDiagnostics(t *testing.T) {
	joined := strings.Join(routerToolDiagnosticLines(), "\n")
	for _, want := range []string{"runner=router", "shell=opencode", "AirLLMRouter"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("router diagnostic missing %q: %s", want, joined)
		}
	}
	if strings.Contains(strings.ToLower(joined), "router.exe") {
		t.Fatalf("router diagnostic must not name fictitious router.exe: %s", joined)
	}
	if code := cmdTool([]string{"-which", "router"}); code != 0 {
		t.Fatalf("tool -which router exit=%d, want 0", code)
	}
	root := t.TempDir()
	raw := []byte(`{"ladder":["script","haiku:medium","sonnet:medium"],"runners":{"haiku":{"kind":"router","model":"air-auto"},"sonnet":{"kind":"claude","model":"sonnet"}}}`)
	if err := os.WriteFile(filepath.Join(root, "run-config.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	active := strings.Join(toolNamesForProduct(root), ",")
	if !strings.Contains(active, "router") {
		t.Fatalf("default tool report missed active router ladder: %s", active)
	}
}
