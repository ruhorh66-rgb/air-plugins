package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func headerTestFacts() planHeaderFacts {
	return planHeaderFacts{
		Branch:                "dev/canonical",
		Head:                  strings.Repeat("a", 40),
		RemoteHead:            strings.Repeat("a", 40),
		InstalledVersion:      "1.9.6",
		InstalledBinarySHA256: strings.Repeat("b", 64),
		ActiveRelease:         "1.9.7",
		NextOpenID:            "N-111_dev136",
	}
}

func TestPlanHeaderManagedRegionPreservesOutsideBytesAndRepeatsNoop(t *testing.T) {
	source := []byte("# AirLegal PLAN\r\n\r\nIntro — original.\r\n<!-- air-worker-managed-header:start -->\r\nold stale header\r\n<!-- air-worker-managed-header:end -->\r\n\r\n<!-- air-worker-plan-nodes -->\r\n- [open] [[N-111_dev136]]\r\nother WIP untouched\r\n")
	facts := headerTestFacts()
	out, changed, err := replaceManagedPlanHeader(source, facts, false)
	if err != nil || !changed {
		t.Fatalf("initial rewrite: changed=%v err=%v", changed, err)
	}
	begin := []byte("<!-- air-worker-managed-header:start -->")
	end := []byte("<!-- air-worker-managed-header:end -->")
	beforePrefix := source[:bytes.Index(source, begin)]
	afterPrefix := out[:bytes.Index(out, begin)]
	if !bytes.Equal(beforePrefix, afterPrefix) {
		t.Fatal("prefix outside managed header changed")
	}
	beforeSuffix := source[bytes.Index(source, end)+len(end):]
	afterSuffix := out[bytes.Index(out, end)+len(end):]
	if !bytes.Equal(beforeSuffix, afterSuffix) {
		t.Fatal("suffix outside managed header changed")
	}
	if !bytes.Contains(out, []byte("N-111_dev136")) || !bytes.Contains(out, []byte("1.9.7")) {
		t.Fatal("expected bounded facts not present")
	}
	again, changed, err := replaceManagedPlanHeader(out, facts, false)
	if err != nil || changed || !bytes.Equal(again, out) {
		t.Fatalf("repeat was not byte-idempotent: changed=%v err=%v", changed, err)
	}
}

func TestPlanHeaderUnownedLegacyPlanRequiresExplicitBootstrap(t *testing.T) {
	original := []byte("# Existing plan\r\n\r\nlegacy header\r\n\r\n## Other work\r\nforeign content\r\n")
	_, changed, err := replaceManagedPlanHeader(original, headerTestFacts(), false)
	if err == nil || changed {
		t.Fatal("unmarked header was guessed or silently modified")
	}
	out, changed, err := replaceManagedPlanHeader(original, headerTestFacts(), true)
	if err != nil || !changed {
		t.Fatalf("explicit bootstrap should succeed for safe plan: %v", err)
	}
	if !bytes.HasPrefix(out, []byte("# Existing plan\r\n<!-- air-worker-managed-header:start -->")) {
		t.Fatal("bootstrap did not insert immediately after the first heading")
	}
	if !bytes.HasSuffix(out, original[len("# Existing plan\r\n"):]) {
		t.Fatal("bootstrap did not preserve remaining legacy plan bytes")
	}
	_, _, err = replaceManagedPlanHeader(out, headerTestFacts(), true)
	if err == nil {
		t.Fatal("bootstrap accepted a previously managed plan")
	}
}

func TestPlanHeaderRejectsUnsafeOrAmbiguousInput(t *testing.T) {
	begin, end := "<!-- air-worker-managed-header:start -->", "<!-- air-worker-managed-header:end -->"
	for _, tc := range []struct {
		name string
		plan []byte
	}{
		{"unmarked", []byte("# Plan\n\nbody")},
		{"duplicate", []byte("# Plan\n" + begin + "\nX\n" + end + "\n" + begin + "\nY\n" + end)},
		{"missingEnd", []byte("# Plan\n" + begin + "\nX\n")},
		{"outOfOrder", []byte("# Plan\n" + end + "\n" + begin)},
		{"control", []byte("# Plan\n" + begin + "\nC0\x07\n" + end)},
		{"invalidUTF8", []byte("# Plan\n" + begin + "\n\xff\n" + end)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := replaceManagedPlanHeader(tc.plan, headerTestFacts(), false); err == nil {
				t.Fatal("accepted malformed plan")
			}
		})
	}
}

func TestPlanHeaderRejectsStaleGitAndInvalidFacts(t *testing.T) {
	base := headerTestFacts()
	for _, testCase := range []struct {
		name string
		edit func(*planHeaderFacts)
	}{
		{"remoteHead", func(x *planHeaderFacts) { x.RemoteHead = strings.Repeat("d", 40) }},
		{"badBranch", func(x *planHeaderFacts) { x.Branch = "topic\ninjected" }},
		{"invalidVersion", func(x *planHeaderFacts) { x.InstalledVersion = "0.11.9A" }},
		{"unknownInstalled", func(x *planHeaderFacts) { x.InstalledVersion = "UNKNOWN" }},
		{"unknownNext", func(x *planHeaderFacts) { x.NextOpenID = "UNKNOWN" }},
		{"controlNode", func(x *planHeaderFacts) { x.NextOpenID = "N-111_\x07" }},
		{"missingBinarySHA", func(x *planHeaderFacts) { x.InstalledBinarySHA256 = "" }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			facts := base
			testCase.edit(&facts)
			if err := validatePlanHeaderFacts(facts); err == nil {
				t.Fatal("malformed/stale facts accepted")
			}
		})
	}
}

func TestPlanHeaderFileCASRejectsConcurrentMutationWithoutClobber(t *testing.T) {
	path := filepath.Join(t.TempDir(), "PLAN.md")
	original := []byte("# Plan\noriginal\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(original)
	expected := hex.EncodeToString(sum[:])
	foreign := []byte("# Plan\nanother owner changed it\n")
	err := writeManagedPlanHeaderCAS(path, expected, []byte("# Plan\nnew\n"), func() error {
		return os.WriteFile(path, foreign, 0o600)
	})
	if err == nil {
		t.Fatal("overwrote concurrent PLAN mutation")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, foreign) {
		t.Fatal("concurrent data were not preserved")
	}
}

func TestPlanHeaderFileCASNoopKeepsBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "PLAN.md")
	body := []byte("# Plan\nsame\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	expected := hex.EncodeToString(sum[:])
	if err := writeManagedPlanHeaderCAS(path, expected, body, nil); err == nil {
		t.Fatal("unlocked PLAN no-op claimed authority before N124 generation")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatal("no-op changed plan")
	}
}

func TestPlanHeaderNativeNextOpenChecksSpineAndDuplicateNumbers(t *testing.T) {
	root := seedPlanNodeProduct(t)
	create := func(title string) {
		t.Helper()
		if code := cmdPlanNodeNew([]string{
			"-product", root, "-title", title, "-parent", "Stage",
			"-owner", "fixture-operator", "-trigger", "fixture test",
			"-done-when", "signed receipt verified",
		}); code != 0 {
			t.Fatalf("node new rc %d", code)
		}
	}
	create("First node")
	create("Second node")
	plan, err := os.ReadFile(filepath.Join(root, "PLAN.md"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := selectCanonicalNextOpenNode(root, plan)
	if err != nil || !strings.HasPrefix(id, "N-001_") {
		t.Fatalf("first native node not chosen: %q %v", id, err)
	}
	// A duplicate numeric N-ID with a different slug must be rejected, not
	// silently sorted and used as though it were a canonical unique identity.
	var duplicate planNode
	for _, entry := range []string{"N-001_rogue.md"} {
		duplicate = planNode{
			ID: strings.TrimSuffix(entry, ".md"), Title: "Rogue",
			Parent: "Stage", Trigger: "test", Owner: "test",
			DoneWhen: "receipt", ReturnTo: "PLAN.md", Status: "open",
			CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
			UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		}
		if err := os.WriteFile(filepath.Join(root, "plan", entry),
			renderPlanNode(duplicate), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, err = selectCanonicalNextOpenNode(root, plan)
	if err == nil {
		t.Fatal("duplicate numeric N-ID accepted")
	}
}

func TestPlanHeaderRejectsControlInNativeNodeEvenIfLintPasses(t *testing.T) {
	root := seedPlanNodeProduct(t)
	if code := cmdPlanNodeNew([]string{
		"-product", root, "-title", "Valid", "-parent", "Stage",
		"-owner", "fixture", "-trigger", "fixture",
		"-done-when", "valid receipt",
	}); code != 0 {
		t.Fatalf("new node rc %d", code)
	}
	nodes, err := listPlanNodes(root)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("nodes err=%v", err)
	}
	path := nodes[0].Path
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the observed U+0007 in private N-134 from a bad PowerShell quote.
	raw = bytes.Replace(raw, []byte("valid receipt"), []byte("valid\x07 receipt"), -1)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	plan, _ := os.ReadFile(filepath.Join(root, "PLAN.md"))
	if _, err := selectCanonicalNextOpenNode(root, plan); err == nil {
		t.Fatal("C0 invalid node treated as canonical")
	}
}

func TestPlanHeaderRejectsEmbeddedMarkersInProseOrCodeFences(t *testing.T) {
	begin, end := "<!-- air-worker-managed-header:start -->", "<!-- air-worker-managed-header:end -->"
	for _, tt := range []struct {
		name string
		plan string
	}{
		{"inline", "# Plan\n" + "example " + begin + "\ncontent\n" + end + "\n"},
		{"fenced", "# Plan\n" + "```md\n" + begin + "\ncontent\n" + end + "\n```\n"},
		{"afterSection", "# Plan\n" + "## Foreign content\n" + begin + "\ncontent\n" + end + "\n"},
		{"indentedFence", "# Plan\n" + "   \x60\x60\x60md\n" + begin + "\ncontent\n" + end + "\n"},
		{"tildeFence", "# Plan\n" + "  ~~~\n" + begin + "\ncontent\n" + end + "\n"},
		{"nestedSection", "# Plan\n" + "### Foreign subsection\n" + begin + "\ncontent\n" + end + "\n"},
		{"secondH1", "# Plan\n" + "# New document heading\n" + begin + "\ncontent\n" + end + "\n"},
		{"secondH1Indented", "# Plan\n" + "   # Nested H1 heading\n" + begin + "\ncontent\n" + end + "\n"},
		{"setextDash", "# Plan\n" + "Section\n---   \n" + begin + "\ncontent\n" + end + "\n"},
		{"setextEqual", "# Plan\n" + "Section\n===  \n" + begin + "\ncontent\n" + end + "\n"},
		{"inlineEnd", "# Plan\n" + begin + "\ncontent\n" + "code " + end + "\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := replaceManagedPlanHeader([]byte(tt.plan), headerTestFacts(), false); err == nil {
				t.Fatal("accepted a managed marker outside the designated header region")
			}
		})
	}
}

func TestPlanHeaderCASRefusesChangedBytesUntilNativeAuthority(t *testing.T) {
	path := filepath.Join(t.TempDir(), "PLAN.md")
	before := []byte("# Plan\nold original\n")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(before)
	sha := hex.EncodeToString(hash[:])
	err := writeManagedPlanHeaderCAS(path, sha, []byte("# Plan\nchanged by unknown owner\n"), nil)
	if err == nil {
		t.Fatal("header mutation allowed without verified native authority and path locking")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("changed bytes published despite authority gate")
	}
}

func TestPlanHeaderSyncCLIRejectsApplyUntilN124Gate(t *testing.T) {
	root := seedPlanNodeProduct(t)
	planPath := filepath.Join(root, "PLAN.md")
	before, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	code := cmdPlanHeaderSync([]string{"-product", root, "-apply", "-json"})
	if code == 0 {
		t.Fatal("CLI applied PLAN without N124 authority")
	}
	after, err := os.ReadFile(planPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("CLI changed legacy PLAN despite missing authority")
	}
}

func TestPlanHeaderSyncCLIDryRunDoesNotMutateForeignData(t *testing.T) {
	root := seedPlanNodeProduct(t)
	if code := cmdPlanNodeNew([]string{
		"-product", root, "-title", "Existing native node",
		"-parent", "Legal", "-owner", "Legal",
		"-trigger", "test", "-done-when", "PASS",
	}); code != 0 {
		t.Fatal("fixture node new")
	}
	plan := filepath.Join(root, "PLAN.md")
	learn := filepath.Join(root, "learn", "events.jsonl")
	beforePlan, err := os.ReadFile(plan)
	if err != nil {
		t.Fatal(err)
	}
	beforeLearn, err := os.ReadFile(learn)
	if err != nil {
		t.Fatal(err)
	}
	nodeFiles, err := filepath.Glob(filepath.Join(root, "plan", "N-*.md"))
	if err != nil || len(nodeFiles) != 1 {
		t.Fatal("native node files missing")
	}
	beforeNode, err := os.ReadFile(nodeFiles[0])
	if err != nil {
		t.Fatal(err)
	}
	code := cmdPlanHeaderSync([]string{"-product", root, "-dry-run", "-json"})
	if code == 0 {
		t.Fatal("CLI incorrectly certified untrusted git/installed attestation")
	}
	afterPlan, err := os.ReadFile(plan)
	if err != nil || !bytes.Equal(beforePlan, afterPlan) {
		t.Fatal("dry-run changed PLAN")
	}
	afterLearn, err := os.ReadFile(learn)
	if err != nil || !bytes.Equal(beforeLearn, afterLearn) {
		t.Fatal("dry-run changed LEARN")
	}
	afterNode, err := os.ReadFile(nodeFiles[0])
	if err != nil || !bytes.Equal(beforeNode, afterNode) {
		t.Fatal("dry-run changed native node")
	}
}

func TestPlanHeaderSyncCLIUnknownActionFailsClosed(t *testing.T) {
	if rc := cmdPlanHeader([]string{"mutate"}); rc == 0 {
		t.Fatal("unknown action accepted")
	}
}

func TestPlanHeaderRemoteURLStrictAllowlist(t *testing.T) {
	for _, u := range []string{
		"https://github.com/example/canonical.git",
		"https://github.com/ruhorh66-rgb/air-plugins.git",
	} {
		if !planHeaderAllowedRemote(u) {
			t.Fatalf("trusted GitHub URL rejected %q", u)
		}
	}
	for _, u := range []string{
		"ssh://git@github.com/example/repo.git",
		"file:///tmp/private.git",
		"https://evil.example.com/example/repo.git",
		"https://github.com.evil.example/repo.git",
		"https://user:password@github.com/example/repo.git",
		"https://github.com/example/repo.git#fragment",
		"https://github.com/example/repo.git?branch=main",
		"https://github.com/example/repo",
	} {
		if planHeaderAllowedRemote(u) {
			t.Fatalf("unsafe remote accepted %q", u)
		}
	}
	for _, r := range []string{"", "..", "github;touch", "remote/name", "-bad", "a.b"} {
		if safePlanHeaderRemoteName(r) {
			t.Fatalf("unsafe remote name accepted %q", r)
		}
	}
	for _, r := range []string{"origin", "github", "private-1"} {
		if !safePlanHeaderRemoteName(r) {
			t.Fatalf("safe remote name rejected %q", r)
		}
	}
}

func TestPlanHeaderActiveReleaseFromMachineSidecarOnly(t *testing.T) {
	root := t.TempDir()
	if _, err := planHeaderActiveRelease(root); err == nil {
		t.Fatal("missing release-slices accepted")
	}
	dir := filepath.Join(root, "plan")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "release-slices.json")
	for _, tc := range []struct {
		name string
		json string
		want bool
	}{
		{"valid", `{"active_release":"1.9.7","releases":[{"version":"1.9.6"},{"version":"1.9.7"}]}`, true},
		{"notInList", `{"active_release":"1.9.7","releases":[{"version":"1.9.6"}]}`, false},
		{"duplicate", `{"active_release":"1.9.7","releases":[{"version":"1.9.7"},{"version":"1.9.7"}]}`, false},
		{"invalidVersion", `{"active_release":"0.11.9A","releases":[{"version":"0.11.9A"}]}`, false},
		{"malformed", `{"active_release":`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tc.json), 0o600); err != nil {
				t.Fatal(err)
			}
			v, err := planHeaderActiveRelease(root)
			if (err == nil) != tc.want {
				t.Fatalf("got %q, err=%v wantvalid=%v", v, err, tc.want)
			}
		})
	}
}

func TestPlanHeaderGitReadNeverHonorsCallerTraceOrHooks(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command("git", "init", "-q", root)
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git unavailable in test environment: %v %s", err, data)
	}
	// GIT_TRACE is a harmless deterministic proof that an uncontrolled
	// inherited Git environment can write outside the requested operation.
	tracePath := filepath.Join(root, "untrusted-inherited-git-trace.txt")
	t.Setenv("GIT_TRACE", tracePath)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.fsmonitor")
	t.Setenv("GIT_CONFIG_VALUE_0", "false")
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git binary unavailable")
	}
	data, err := os.ReadFile(gitBin)
	if err != nil {
		t.Fatal(err)
	}
	binSHA := sha256.Sum256(data)
	pin := planHeaderGitTool{Path: gitBin, SHA256: hex.EncodeToString(binSHA[:])}
	result, err := planHeaderGitReadPinned(pin, root, "rev-parse", "--is-inside-work-tree")
	if err != nil || result != "true" {
		t.Fatalf("Git read-only preflight failed: %q, %v", result, err)
	}
	if _, err := os.Stat(tracePath); !os.IsNotExist(err) {
		t.Fatal("Git inherited untrusted environment and wrote a trace")
	}
}

func planHeaderTestGitPin(t *testing.T) planHeaderGitTool {
	t.Helper()
	path, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git binary unavailable")
	}
	path, err = filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	return planHeaderGitTool{Path: path, SHA256: hex.EncodeToString(digest[:])}
}

func TestPlanHeaderUnpinnedGitNeverUsesPATH(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := planHeaderGitRead(t.TempDir(), "rev-parse", "--show-toplevel"); err == nil {
		t.Fatal("unpinned Git executable was allowed")
	}
}

func TestPlanHeaderGitPinRejectsWrongSHAAndRelativePath(t *testing.T) {
	pin := planHeaderTestGitPin(t)
	if _, err := planHeaderGitReadPinned(planHeaderGitTool{Path: "git", SHA256: pin.SHA256},
		t.TempDir(), "rev-parse", "--is-inside-work-tree"); err == nil {
		t.Fatal("relative Git binary accepted")
	}
	pin.SHA256 = strings.Repeat("0", 64)
	if _, err := planHeaderGitReadPinned(pin, t.TempDir(),
		"rev-parse", "--is-inside-work-tree"); err == nil {
		t.Fatal("untrusted Git binary SHA accepted")
	}
}

func TestPlanHeaderRepoRootMustBeAuthorizedProduct(t *testing.T) {
	pin := planHeaderTestGitPin(t)
	root := filepath.Join(t.TempDir(), "canonical")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	git := exec.Command(pin.Path, "init", "-q", root)
	if data, err := git.CombinedOutput(); err != nil {
		t.Skipf("Git fixtures unavailable: %v %s", err, data)
	}
	if err := planHeaderValidateRepository(root, pin); err != nil {
		t.Fatalf("repo root rejected: %v", err)
	}
	foreign := filepath.Join(root, "unrelated")
	if err := os.MkdirAll(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := planHeaderValidateRepository(foreign, pin); err == nil {
		t.Fatal("ancestor Git repo was used as the foreign product's identity")
	}
	product := filepath.Join(root, "air-worker")
	if err := os.MkdirAll(product, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := planHeaderValidateRepository(product, pin); err != nil {
		t.Fatalf("legitimate one-layer product root rejected: %v", err)
	}
}

func TestPlanHeaderReadRejectsSymlinkLeaf(t *testing.T) {
	root := t.TempDir()
	foreign := filepath.Join(t.TempDir(), "foreign-content")
	if err := os.WriteFile(foreign, []byte("# Foreign\nsecret text"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "PLAN.md")
	if err := os.Symlink(foreign, path); err != nil {
		t.Skipf("symlink privilege missing: %v", err)
	}
	if _, err := planHeaderReadPlan(root); err == nil {
		t.Fatal("PLAN symlink leaf allowed")
	}
}

func TestPlanHeaderReadPlanRegularFileNoMutation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "PLAN.md")
	body := []byte("# Canonical PLAN\r\nother bytes\r\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := planHeaderReadPlan(root)
	if err != nil || !bytes.Equal(body, got) {
		t.Fatalf("regular PLAN not read intact: %v", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(body, after) {
		t.Fatal("read-only PLAN read changed bytes")
	}
}

func TestPlanHeaderRemoteReadRejectsUntrustedURLWithoutNetwork(t *testing.T) {
	pin := planHeaderTestGitPin(t)
	for _, url := range []string{"file:///tmp/repo", "ext::open", "ssh://git@github.com/test/repo.git",
		"https://github.com.evil.com/test/repo.git"} {
		if _, err := planHeaderGitRemoteRead(pin, url, "refs/heads/main"); err == nil {
			t.Fatalf("unsafe remote URL accepted %q", url)
		}
	}
	if _, err := planHeaderGitRemoteRead(pin, "https://github.com/test/repo.git",
		"refs/heads/../malicious"); err == nil {
		t.Fatal("unsafe ref accepted")
	}
}
