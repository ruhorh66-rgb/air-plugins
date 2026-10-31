package main

import (
	"strings"
	"testing"
)

func TestVersionCommandMatchesReleaseTarget(t *testing.T) {
	out, code := captureStdout(t, func() int { return run([]string{"version"}) })
	if code != 0 {
		t.Fatalf("version exit code=%d, want 0", code)
	}
	if got, want := strings.TrimSpace(out), "air-worker 0.11.11-rc.2"; got != want {
		t.Fatalf("version output=%q, want %q", got, want)
	}
}
