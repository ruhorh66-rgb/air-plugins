//go:build windows

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A junction can be created in a disposable Windows test directory without
// invoking UAC. If the host denies creating it, the security test is SKIPPED,
// not silently counted as coverage. Never point it at production data.
func TestPlanHeaderRejectsJunctionPlanDirectory(t *testing.T) {
	root := t.TempDir()
	foreign := t.TempDir()
	plan := []byte("# Fixture PLAN\n\n" + planNodeMarker + "\n")
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), plan, 0600); err != nil {
		t.Fatal(err)
	}
	release := []byte(`{"active_release":"1.9.7","releases":[{"version":"1.9.7"}]}`)
	if err := os.WriteFile(filepath.Join(foreign, "release-slices.json"), release, 0600); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(root, "plan")
	out, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, foreign).CombinedOutput()
	if err != nil {
		t.Skipf("cannot construct disposable NTFS junction: %v; %s", err, string(out))
	}
	// On some Windows Go versions EvalSymlinks keeps the junction spelling.
	// Prove the *same physical file* is reached through both paths instead.
	foreignInfo, err := os.Stat(filepath.Join(foreign, "release-slices.json"))
	if err != nil {
		t.Fatal(err)
	}
	linkedInfo, err := os.Stat(filepath.Join(junction, "release-slices.json"))
	if err != nil || !os.SameFile(foreignInfo, linkedInfo) {
		t.Fatalf("junction fixture does not alias the foreign file: %v", err)
	}
	linkInfo, err := os.Lstat(junction)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("junction fixture verified: mode=%s, symlink=%v", linkInfo.Mode(), linkInfo.Mode()&os.ModeSymlink != 0)

	// Neither a release version nor a next-N pointer may come through a
	// junction that redirects the canonical plan/ directory out of the root.
	if v, err := planHeaderActiveRelease(root); err == nil {
		t.Fatalf("accepted release data through redirected plan/ junction: %s", v)
	}
	if id, err := selectCanonicalNextOpenNode(root, plan); err == nil {
		t.Fatalf("accepted native node state through redirected plan/ junction: %s", id)
	}
	got, err := os.ReadFile(filepath.Join(root, "PLAN.md"))
	if err != nil || !bytes.Equal(got, plan) {
		t.Fatalf("junction denial mutated PLAN: %v", err)
	}
	gotForeign, err := os.ReadFile(filepath.Join(foreign, "release-slices.json"))
	if err != nil || !bytes.Equal(gotForeign, release) {
		t.Fatalf("junction denial mutated external release sidecar: %v", err)
	}
}

func TestPlanHeaderRejectsJunctionProductRoot(t *testing.T) {
	physical := t.TempDir()
	parent := t.TempDir()
	body := []byte("# Canonical original\\n")
	if err := os.WriteFile(filepath.Join(physical, "PLAN.md"), body, 0600); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(parent, "product")
	out, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, physical).CombinedOutput()
	if err != nil {
		t.Skipf("cannot create disposable Windows junction: %v; %s", err, string(out))
	}
	targetInfo, err := os.Stat(filepath.Join(physical, "PLAN.md"))
	if err != nil {
		t.Fatal(err)
	}
	viaInfo, err := os.Stat(filepath.Join(junction, "PLAN.md"))
	if err != nil || !os.SameFile(targetInfo, viaInfo) {
		t.Fatalf("junction did not alias the same physical PLAN: %v", err)
	}
	if err := planHeaderValidateAncestors(junction); err == nil {
		t.Fatal("junction redirected product root was granted PLAN authority")
	}
	if read, err := planHeaderReadPlan(junction); err == nil {
		t.Fatalf("PLAN read followed an untrusted product-root junction: %q", read)
	}
	original, err := os.ReadFile(filepath.Join(physical, "PLAN.md"))
	if err != nil || !bytes.Equal(original, body) {
		t.Fatalf("product-root junction guard modified original PLAN: %v", err)
	}
}

func TestPlanHeaderRejectsOversizedNodeBeforeParsing(t *testing.T) {
	root := seedPlanNodeProduct(t)
	if code := cmdPlanNodeNew([]string{
		"-product", root, "-title", "Witness", "-parent", "Stage",
		"-owner", "fixture", "-trigger", "oversize source", "-done-when", "reject",
	}); code != 0 {
		t.Fatalf("fixture native node new rc=%d", code)
	}
	nodes, err := listPlanNodes(root)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("expected one native node: %v", err)
	}
	plan, err := os.ReadFile(filepath.Join(root, "PLAN.md"))
	if err != nil {
		t.Fatal(err)
	}
	oversize := bytes.Repeat([]byte("X"), 1024*1024+1)
	if err := os.WriteFile(nodes[0].Path, oversize, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := selectCanonicalNextOpenNode(root, plan)
	if err == nil || !strings.Contains(err.Error(), "exceeds preflight byte limit") {
		t.Fatalf("oversized untrusted node should be rejected before parsing, got=%q err=%v", got, err)
	}
	readback, err := os.ReadFile(nodes[0].Path)
	if err != nil || !bytes.Equal(readback, oversize) {
		t.Fatalf("preflight changed foreign oversized test bytes: %v", err)
	}
}
