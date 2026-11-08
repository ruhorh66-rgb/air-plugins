//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func TestJobSourceHeadVolumeGUIDPath(t *testing.T) {
	root := t.TempDir()
	runGit := func(stdin string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if stdin != "" {
			cmd.Stdin = strings.NewReader(stdin)
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	runGit("", "init", "--quiet")
	blob := runGit("test", "hash-object", "-w", "--stdin")
	tree := runGit("100644 blob "+blob+"\tfile\n", "mktree")
	head := runGit("tree "+tree+"\nauthor Probe <probe@example.invalid> 1700000000 +0000\ncommitter Probe <probe@example.invalid> 1700000000 +0000\n\nGUID path test\n", "hash-object", "-w", "-t", "commit", "--stdin")
	runGit(head, "update-ref", "HEAD", head)

	dir, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetFinalPathNameByHandleW")
	buf := make([]uint16, 32768)
	const volumeNameGUID = 0x1
	n, _, callErr := proc.Call(dir.Fd(), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), volumeNameGUID)
	if n == 0 || int(n) >= len(buf) {
		t.Fatalf("GetFinalPathNameByHandleW(VOLUME_NAME_GUID) failed: %v", callErr)
	}
	guidRoot := syscall.UTF16ToString(buf[:n])
	if !strings.HasPrefix(guidRoot, `\\?\Volume{`) {
		t.Fatalf("expected volume-GUID path, got %q", guidRoot)
	}
	guidRoot = filepath.Clean(guidRoot)
	if got := jobSourceHead(guidRoot); got != head {
		t.Fatalf("jobSourceHead(%q) = %q, want committed HEAD %q", guidRoot, got, head)
	}
}
