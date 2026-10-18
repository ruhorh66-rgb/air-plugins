package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// DEV-136 reads release-slices.json as a machine authority input.
// Go's json.Unmarshal silently accepts duplicate object keys by keeping the
// last value. That is NOT an unambiguous release authority, even if the last
// active_release happens to be syntactically valid. This is temp-only TDD.
func TestPlanHeaderReleaseSidecarRejectsAmbiguousJSON(t *testing.T) {
	root := t.TempDir()
	planDir := filepath.Join(root, "plan")
	if err := os.Mkdir(planDir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(planDir, "release-slices.json")
	for _, tc := range []struct {
		name   string
		body   string
		accept bool
	}{
		{"valid-simple", `{"active_release":"1.9.7","releases":[{"version":"1.9.6"},{"version":"1.9.7"}]}`, true},
		{"valid-with-extra-fields", `{"schema":"existing-format/v1","active_release":"1.9.7","releases":[{"version":"1.9.7","status":"open"}],"metadata":{"unknown_field":true}}`, true},
		{"duplicate-active-release", `{"active_release":"1.9.6","active_release":"1.9.7","releases":[{"version":"1.9.7"}]}`, false},
		{"case-fold-duplicate-active-release", `{"ACTIVE_RELEASE":"1.9.6","active_release":"1.9.7","releases":[{"version":"1.9.7"}]}`, false},
		{"escaped-duplicate-active-release", `{"\u0061ctive_release":"1.9.6","active_release":"1.9.7","releases":[{"version":"1.9.7"}]}`, false},
		{"deeply-nested-unknown-metadata", `{"active_release":"1.9.7","releases":[{"version":"1.9.7"}],"extra":` + strings.Repeat("[", 70) + "0" + strings.Repeat("]", 70) + "}", false},
		{"invalid-utf8-in-unknown-metadata", `{"active_release":"1.9.7","releases":[{"version":"1.9.7"}],"extra":"` + string([]byte{0xff}) + `"}`, false},
		{"duplicate-releases-list", `{"active_release":"1.9.7","releases":[{"version":"1.9.6"}],"releases":[{"version":"1.9.7"}]}`, false},
		{"duplicate-version-in-entry", `{"active_release":"1.9.7","releases":[{"version":"1.9.6","version":"1.9.7"}]}`, false},
		{"duplicate-unknown-nested-key", `{"active_release":"1.9.7","releases":[{"version":"1.9.7","notes":{"gate":false,"gate":true}}]}`, false},
		{"two-json-values", `{"active_release":"1.9.7","releases":[{"version":"1.9.7"}]} {"active_release":"1.9.6"}`, false},
		{"too-many-bytes", `{"active_release":"1.9.7","releases":[{"version":"1.9.7"}],"extra":"` + strings.Repeat("x", (1<<20)+1) + `"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(file, []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			active, err := planHeaderActiveRelease(root)
			if (err == nil) != tc.accept {
				t.Fatalf("read release authority: active=%q err=%v should_accept=%v", active, err, tc.accept)
			}
			if tc.accept && active != "1.9.7" {
				t.Fatalf("valid sidecar read wrong active release: %q", active)
			}
		})
	}
}
