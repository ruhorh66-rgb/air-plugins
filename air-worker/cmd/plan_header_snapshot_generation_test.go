package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Two authentic Go binaries are built from consecutive commits in an
// isolated temporary Git repository. After A's validated snapshot is read,
// the SAME pathname is replaced by binary B with a different VCS revision.
// The SHA and embedded VCS identity decoded from A's immutable byte snapshot
// must remain A's, not a mixed hash(A)+revision(B) identity.
func TestPlanHeaderStaticSnapshotCannotMixPathGenerations(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("isolated Git fixture requires git: %v", err)
	}
	root := t.TempDir()
	buildPath := filepath.Join(t.TempDir(), "snapshot-generation"+func() string {
		return filepath.Ext(executableName())
	}())
	manifest := "module example.com/airworker-dev136-snapshot-fixture\n\ngo 1.23\n"
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GOWORK=off")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %s: %v: %s", name, strings.Join(args, " "), err, out)
		}
	}
	run(git, "init", "-q")
	sentinel := filepath.Join(root, "WOULD_HAVE_EXECUTED.txt")
	writeSource := func(mark string) {
		t.Helper()
		// Neither binary is executed: if it were, the sentinel proves it.
		content := "package main\nimport \"os\"\n" +
			"func main(){ _ = os.WriteFile(" + strconv.Quote(sentinel) +
			", []byte(" + strconv.Quote(mark) + "), 0600) }\n"
		if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(msg string) {
		t.Helper()
		run(git, "add", "go.mod", "main.go")
		run(git, "-c", "user.name=DEV136Fixture", "-c",
			"user.email=dev136-fixture@example.invalid", "-c",
			"commit.gpgsign=false", "commit", "-qm", msg)
	}
	writeSource("A")
	commit("immutable build A")
	run("go", "build", "-buildvcs=true", "-o", buildPath, ".")
	bytesA, err := planHeaderReadBoundedExeSnapshot(buildPath)
	if err != nil {
		t.Fatalf("cannot read sealed binary A: %v", err)
	}
	idA, err := planHeaderStaticIdentityFromSnapshot(buildPath, "0.11.8", bytesA)
	if err != nil {
		t.Fatalf("Go VCS identity A unavailable: %v", err)
	}
	if idA.Revision == "" || idA.Modified == nil || *idA.Modified {
		t.Fatalf("A must be a clean VCS-linked Go build: %+v", idA)
	}
	writeSource("B")
	commit("immutable build B")
	// Deliberately replace the FILE PATH after snapshot A, before parsing it.
	run("go", "build", "-buildvcs=true", "-o", buildPath, ".")
	idB, err := planHeaderStaticBinaryIdentity(buildPath, "0.11.8")
	if err != nil {
		t.Fatalf("Go VCS identity B unavailable: %v", err)
	}
	if idA.Revision == idB.Revision || idA.SHA256 == idB.SHA256 {
		t.Fatalf("fixture not two distinct binary generations: A=%+v B=%+v", idA, idB)
	}
	afterReplacement, err := planHeaderStaticIdentityFromSnapshot(
		buildPath, "0.11.8", bytesA)
	if err != nil {
		t.Fatal(err)
	}
	expectedSHA := sha256.Sum256(bytesA)
	if afterReplacement.SHA256 != hex.EncodeToString(expectedSHA[:]) ||
		afterReplacement.SHA256 != idA.SHA256 ||
		afterReplacement.Revision != idA.Revision ||
		afterReplacement.Revision == idB.Revision {
		t.Fatalf("mixed SHA(A) and revision(B) after path swap: A=%+v B=%+v final=%+v",
			idA, idB, afterReplacement)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("DEV136 executed one of the cache generation fixtures: %v", err)
	}
}
