package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func releaseSlicesFixture(t *testing.T) (string, map[string]any) {
	t.Helper()
	root := t.TempDir()
	raw, err := os.ReadFile(filepath.Join("..", "plan", "release-slices.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "plan"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "plan", "release-slices.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return root, doc
}

func saveReleaseSlices(t *testing.T, root string, doc map[string]any) {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "plan", "release-slices.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func releaseAt(doc map[string]any, i int) map[string]any {
	return doc["releases"].([]any)[i].(map[string]any)
}
func stepAt(doc map[string]any, ri, si int) map[string]any {
	return releaseAt(doc, ri)["steps"].([]any)[si].(map[string]any)
}
func hasReleaseSliceRule(s releaseSlicesSummary, rule string) bool {
	for _, p := range s.Problems {
		if p.Rule == rule {
			return true
		}
	}
	return false
}

func TestInspectReleaseSlicesValidAndAbsent(t *testing.T) {
	root, _ := releaseSlicesFixture(t)
	s := inspectReleaseSlices(root)
	if !s.Found || s.ActiveVersion != "0.11.9" || s.NextStep != "R0.11.9-01" || s.ReleasesCount != 9 || s.StepsCount != 23 || len(s.Problems) != 0 {
		t.Fatalf("unexpected summary: %+v", s)
	}
	legacy := t.TempDir()
	if err := os.WriteFile(filepath.Join(legacy, "PLAN.md"), []byte("legacy plan"), 0o600); err != nil {
		t.Fatal(err)
	}
	s = inspectReleaseSlices(legacy)
	if s.Found || len(s.Problems) != 0 {
		t.Fatalf("absent sidecar should be silent: %+v", s)
	}
}

func TestInspectReleaseSlicesContracts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
		rule   string
	}{
		{"bad semver", func(d map[string]any) { releaseAt(d, 0)["version"] = "1.20" }, "version"},
		{"numeric order", func(d map[string]any) { releaseAt(d, 1)["version"] = "0.11.8" }, "version-order"},
		{"future dependency", func(d map[string]any) { releaseAt(d, 0)["dependencies"] = []any{"0.11.10"} }, "dependency"},
		{"missing receipt", func(d map[string]any) { stepAt(d, 0, 0)["receipt"] = "" }, "required"},
		{"budget", func(d map[string]any) { stepAt(d, 0, 0)["budget_minutes"] = float64(16) }, "budget"},
		{"missing owner", func(d map[string]any) { stepAt(d, 0, 0)["owner"] = "" }, "required"},
		{"missing judge", func(d map[string]any) { stepAt(d, 0, 0)["judge"] = "" }, "required"},
		{"traversal", func(d map[string]any) { stepAt(d, 0, 0)["receipt"] = "../x.json" }, "receipt-path"},
		{"absolute", func(d map[string]any) { stepAt(d, 0, 0)["receipt"] = "C:/x.json" }, "receipt-path"},
		{"no open", func(d map[string]any) { stepAt(d, 0, 0)["status"] = "planned" }, "open-step"},
		{"extra open", func(d map[string]any) { stepAt(d, 0, 1)["status"] = "open" }, "open-step"},
		{"duplicate receipt", func(d map[string]any) { stepAt(d, 0, 1)["receipt"] = stepAt(d, 0, 0)["receipt"] }, "receipt-duplicate"},
		{"two active", func(d map[string]any) { releaseAt(d, 1)["status"] = "active" }, "active-release"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, doc := releaseSlicesFixture(t)
			tt.mutate(doc)
			saveReleaseSlices(t, root, doc)
			if s := inspectReleaseSlices(root); !hasReleaseSliceRule(s, tt.rule) {
				t.Fatalf("expected %s, got %+v", tt.rule, s)
			}
		})
	}
}

func TestInspectReleaseSlicesRejectsDuplicateJSONKeysAndOversize(t *testing.T) {
	root, _ := releaseSlicesFixture(t)
	path := filepath.Join(root, "plan", "release-slices.json")
	if err := os.WriteFile(path, []byte(`{"schema":"air-worker.release-slices/v1","schema":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if s := inspectReleaseSlices(root); !hasReleaseSliceRule(s, "json") {
		t.Fatalf("duplicate keys accepted: %+v", s)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat(" ", (1<<20)+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if s := inspectReleaseSlices(root); !hasReleaseSliceRule(s, "size") {
		t.Fatalf("oversized file accepted: %+v", s)
	}
}

func TestInspectReleaseSlicesRejectsSymlinkReceiptAncestor(t *testing.T) {
	root, doc := releaseSlicesFixture(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "docs")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	saveReleaseSlices(t, root, doc)
	if s := inspectReleaseSlices(root); !hasReleaseSliceRule(s, "receipt-path") {
		t.Fatalf("symlink ancestor accepted: %+v", s)
	}
}
