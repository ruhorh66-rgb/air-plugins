package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func planAuditTestNode(id, digest string) planAuditNode {
	match := planNodeIDRE.FindStringSubmatch(id)
	if match == nil {
		panic("bad test fixture")
	}
	return planAuditNode{ID: id, Number: match[1], SHA256: digest, BlobOID: strings.Repeat("b", 40)}
}

func TestPlanAuthorityAuditComparison(t *testing.T) {
	one := planAuditSnapshot{Head: strings.Repeat("a", 40), Nodes: []planAuditNode{
		planAuditTestNode("N-119_base", strings.Repeat("1", 64)),
		planAuditTestNode("N-120_next", strings.Repeat("2", 64)),
	}}
	clone := planAuditSnapshot{Head: strings.Repeat("c", 40), Nodes: append([]planAuditNode(nil), one.Nodes...)}
	clone.Nodes[0], clone.Nodes[1] = clone.Nodes[1], clone.Nodes[0]
	for _, tc := range []struct {
		name   string
		peer   planAuditSnapshot
		want   string
		reason string
	}{
		{"same-content-order-independent", clone, "NO_COLLISIONS_LOCAL_ONLY", ""},
		{"different-numeric-slug", planAuditSnapshot{Head: clone.Head, Nodes: []planAuditNode{
			planAuditTestNode("N-119_base", strings.Repeat("1", 64)),
			planAuditTestNode("N-120_other", strings.Repeat("2", 64)),
		}}, "CONFLICT", "numeric_id_reallocated"},
		{"same-id-different-content", planAuditSnapshot{Head: clone.Head, Nodes: []planAuditNode{
			planAuditTestNode("N-119_base", strings.Repeat("1", 64)),
			planAuditTestNode("N-120_next", strings.Repeat("3", 64)),
		}}, "CONFLICT", "same_id_content_diverged"},
		{"new-id-does-not-collide", planAuditSnapshot{Head: clone.Head, Nodes: []planAuditNode{
			planAuditTestNode("N-119_base", strings.Repeat("1", 64)),
			planAuditTestNode("N-121_after", strings.Repeat("4", 64)),
		}}, "NO_COLLISIONS_LOCAL_ONLY", ""},
		{"repeated-number-in-peer", planAuditSnapshot{Head: clone.Head, Nodes: []planAuditNode{
			planAuditTestNode("N-120_next", strings.Repeat("2", 64)),
			planAuditTestNode("N-120_other", strings.Repeat("3", 64)),
		}}, "NOT_PROVEN", ""},
		{"untrusted-hash", planAuditSnapshot{Head: clone.Head, Nodes: []planAuditNode{
			planAuditTestNode("N-119_base", "NOT_A_SHA"),
		}}, "NOT_PROVEN", ""},
		{"invalid-blob-oid", planAuditSnapshot{Head: clone.Head, Nodes: []planAuditNode{{
			ID: "N-119_base", Number: "119", SHA256: strings.Repeat("1", 64), BlobOID: "untrusted",
		}}}, "NOT_PROVEN", ""},
		{"numeric-field-lie", planAuditSnapshot{Head: clone.Head, Nodes: []planAuditNode{{
			ID: "N-119_base", Number: "120", SHA256: strings.Repeat("1", 64), BlobOID: strings.Repeat("b", 40),
		}}}, "NOT_PROVEN", ""},
		{"missing-peer-commit", planAuditSnapshot{Head: "", Nodes: clone.Nodes}, "NOT_PROVEN", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, collisions, err := planAuditCompare(one, tc.peer)
			if status != tc.want {
				t.Fatalf("want %s, got %s (%v)", tc.want, status, err)
			}
			if status == "NOT_PROVEN" && err == nil {
				t.Fatal("must give a reason when identity cannot be proved")
			}
			if tc.reason != "" && (len(collisions) != 1 || collisions[0].Reason != tc.reason ||
				collisions[0].Number != "120") {
				t.Fatalf("bad collision: %+v", collisions)
			}
			if status == "NO_COLLISIONS_LOCAL_ONLY" && len(collisions) != 0 {
				t.Fatalf("nonempty conflict: %+v", collisions)
			}
		})
	}
}

func TestPlanAuthorityAuditImmutableGitSnapshots(t *testing.T) {
	_, a, b := n124ProbeClones(t)
	source := n124ProbeMustGit(t, a, "", "rev-parse", "HEAD")
	beforeA, beforeB := n124Snapshot(t, a), n124Snapshot(t, b)
	c, err := planAuditReadSnapshot(a, "", source)
	if err != nil {
		t.Fatalf("canonical read failed: %v", err)
	}
	if len(c.Nodes) != 1 || c.Nodes[0].ID != "N-119_base" ||
		c.Nodes[0].Number != "119" || !planAuditSHA256Valid(c.Nodes[0].SHA256) {
		t.Fatalf("wrong native snapshot: %+v", c)
	}
	if _, err := planAuditReadSnapshot(a, "", strings.Repeat("f", 40)); err == nil {
		t.Fatal("missing immutable object must not be inferred from local PLAN")
	}
	// Source-only fixture builds two independently committed descendants.
	n124SeedNode(t, a, "N-120_first", "first")
	n124ProbeMustGit(t, a, "", "add", ".")
	n124ProbeMustGit(t, a, "", "commit", "-m", "first claim")
	h1 := n124ProbeMustGit(t, a, "", "rev-parse", "HEAD")
	n124SeedNode(t, b, "N-120_second", "second")
	n124ProbeMustGit(t, b, "", "add", ".")
	n124ProbeMustGit(t, b, "", "commit", "-m", "second claim")
	h2 := n124ProbeMustGit(t, b, "", "rev-parse", "HEAD")
	s1, err := planAuditReadSnapshot(a, "", h1)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := planAuditReadSnapshot(b, "", h2)
	if err != nil {
		t.Fatal(err)
	}
	got, conflicts, err := planAuditCompare(s1, s2)
	if err != nil || got != "CONFLICT" || len(conflicts) != 1 ||
		conflicts[0].Number != "120" || conflicts[0].Reason != "numeric_id_reallocated" {
		t.Fatalf("cross-branch collision not recognized: %v %v %+v", got, err, conflicts)
	}
	// A read-only audit of frozen old SHA must not project either client WIP.
	oldA, err := planAuditReadSnapshot(a, "", source)
	if err != nil || !reflect.DeepEqual(oldA.Nodes, c.Nodes) {
		t.Fatal("immutable old commit changed")
	}
	if reflect.DeepEqual(beforeA, n124Snapshot(t, a)) || reflect.DeepEqual(beforeB, n124Snapshot(t, b)) {
		t.Fatal("fixture mutation precondition did not occur")
	}
}

func TestPlanAuthorityAuditAlwaysFailClosed(t *testing.T) {
	for _, args := range [][]string{
		{"authority"},
		{"authority", "audit", "-json"},
		{"authority", "audit", "-canonical-branch", "../injected", "-peer-branch", "dev/other", "-canonical-head", strings.Repeat("a", 40), "-peer-head", strings.Repeat("b", 40), "-json"},
		{"authority", "audit", "-canonical-branch", "dev/same", "-peer-branch", "dev/same", "-canonical-head", strings.Repeat("a", 40), "-peer-head", strings.Repeat("b", 40), "-json"},
	} {
		t.Run(fmt.Sprintf("input-%d", len(args)), func(t *testing.T) {
			var rc int
			var data string
			data, rc = captureStdout(t, func() int { return cmdPlan(args) })
			if rc != 2 {
				t.Fatalf("unexpected native authority rc=%d data=%s", rc, data)
			}
			if strings.Contains(data, `"authority_granted":true`) || strings.Contains(data, `"changed":true`) {
				t.Fatal("audit must never grant authority or mutate a plan")
			}
			if len(args) > 1 && args[1] == "audit" && strings.Contains(data, `"schema"`) {
				var result planAuditResult
				if err := json.Unmarshal([]byte(data), &result); err != nil || result.ExitCode != 2 ||
					result.AuthorityGranted || result.Changed ||
					result.Status == "PASS" || result.Status == "ACCEPTED" {
					t.Fatalf("unsafe machine receipt: %+v err=%v", result, err)
				}
			}
		})
	}
}

func TestPlanAuthorityAuditBranchValidation(t *testing.T) {
	for _, branch := range []string{"", "../a", "a..b", "/root", "a/", "-a", "a.lock", "a//b", "a?b", "a b", "a\\b", "a@{b", "a\nb"} {
		if planAuditBranchName(branch) {
			t.Fatalf("accepted unsafe branch: %q", branch)
		}
	}
	for _, branch := range []string{"main", "dev/airworker-0119", "release/v0.11.9"} {
		if !planAuditBranchName(branch) {
			t.Fatalf("rejected branch: %q", branch)
		}
	}
}

func TestPlanAuthorityAuditGitConfigFailClosed(t *testing.T) {
	for _, tc := range []struct{ name, key, value string }{
		{"url-rewrite", "url.https://malicious.invalid/.insteadOf", "https://github.com/"},
		{"credential-helper", "credential.helper", "unsafe-helper"},
		{"include", "include.path", "untrusted-config"},
		{"http-proxy", "http.proxy", "http://malicious.invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, root, _ := n124ProbeClones(t)
			if err := planAuditLocalGitConfigSafe(root); err != nil {
				t.Fatalf("safe initial Git config must be readable: %v", err)
			}
			n124ProbeMustGit(t, root, "", "config", "--local", tc.key, tc.value)
			if err := planAuditLocalGitConfigSafe(root); err == nil {
				t.Fatalf("untrusted Git configuration %s was accepted", tc.key)
			}
		})
	}
}

func TestPlanAuthorityAuditBrokenCommittedSpine(t *testing.T) {
	_, root, _ := n124ProbeClones(t)
	head := n124ProbeMustGit(t, root, "", "rev-parse", "HEAD")
	if _, err := planAuditReadSnapshot(root, "", head); err != nil {
		t.Fatal(err)
	}
	// A valid node file without the matching PLAN marker is not a complete
	// canonical generation. This is a private disposable cloned fixture.
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte("# no native marker\n"), 0600); err != nil {
		t.Fatal(err)
	}
	n124ProbeMustGit(t, root, "", "add", "PLAN.md")
	n124ProbeMustGit(t, root, "", "commit", "-m", "tamper PLAN marker")
	broken := n124ProbeMustGit(t, root, "", "rev-parse", "HEAD")
	if _, err := planAuditReadSnapshot(root, "", broken); err == nil {
		t.Fatal("snapshot without canonical plan marker must be rejected")
	}
	if _, err := planAuditReadSnapshot(root, "", head); err != nil {
		t.Fatal("immutable previous generation must remain readable:", err)
	}
}

func TestPlanAuthorityAuditRejectsFakeSpineLine(t *testing.T) {
	_, root, _ := n124ProbeClones(t)
	file := filepath.Join(root, "PLAN.md")
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	original := string(raw)
	lines := strings.Split(original, "\n")
	found := false
	for i, line := range lines {
		if strings.Contains(line, "[[N-119_base]]") {
			// Keep the marker and link once, but forge owner/status/title.
			lines[i] = "- [closed] fake-parent • owner:attacker • [[N-119_base]] • forged PASS"
			found = true
			break
		}
	}
	if !found {
		t.Fatal("fixture missing native spine entry")
	}
	if err := os.WriteFile(file, []byte(strings.Join(lines, "\n")), 0600); err != nil {
		t.Fatal(err)
	}
	n124ProbeMustGit(t, root, "", "add", "PLAN.md")
	n124ProbeMustGit(t, root, "", "commit", "-m", "forged spine line")
	sha := n124ProbeMustGit(t, root, "", "rev-parse", "HEAD")
	if _, err := planAuditReadSnapshot(root, "", sha); err == nil {
		t.Fatal("forged status/owner in PLAN line must never pass read-only native audit")
	}
}

func TestPlanAuthorityAuditRejectsDuplicateSpineLink(t *testing.T) {
	_, root, _ := n124ProbeClones(t)
	file := filepath.Join(root, "PLAN.md")
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	// Keep the correct native spine line while injecting a conflicting
	// second reference with the same numeric/whole ID elsewhere in PLAN.
	injected := append(append([]byte{}, raw...),
		[]byte("\n- [closed] forged-parent | owner:attacker | [[N-119_base]] | fake PASS\n")...)
	if err := os.WriteFile(file, injected, 0600); err != nil {
		t.Fatal(err)
	}
	n124ProbeMustGit(t, root, "", "add", "PLAN.md")
	n124ProbeMustGit(t, root, "", "commit", "-m", "duplicate native link")
	sha := n124ProbeMustGit(t, root, "", "rev-parse", "HEAD")
	if _, err := planAuditReadSnapshot(root, "", sha); err == nil {
		t.Fatal("duplicate native ID link must be rejected even with one exact spine line")
	}
}
