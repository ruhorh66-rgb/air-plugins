//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCodeBundleIntegrityAndInstall(t *testing.T) {
	archive := filepath.Join("..", "vendor", "opencode", openCodeBundleArchive)
	h, err := sha256File(archive)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(h, openCodeBundleArchiveSHA256) {
		t.Fatalf("archive SHA=%s want %s", h, openCodeBundleArchiveSHA256)
	}
	stage, err := stageRouterRuntimePayload(filepath.Join("..", "bin"))
	if err != nil {
		t.Fatal(err)
	}
	defer stage.cleanup()
	home := t.TempDir()
	if err := installRouterRuntimePayload(stage, home); err != nil {
		t.Fatal(err)
	}

	installed := filepath.Join(home, "tools", "opencode", "opencode.exe")
	if err := verifyOpenCodeBinary(installed); err != nil {
		t.Fatal(err)
	}
	bridge := filepath.Join(home, "tools", "router_stream_bridge.py")
	if st, err := os.Stat(bridge); err != nil || st.IsDir() || st.Size() == 0 {
		t.Fatalf("bridge not installed: stat=%v err=%v", st, err)
	}
}
