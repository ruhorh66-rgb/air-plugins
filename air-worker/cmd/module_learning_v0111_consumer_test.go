package main

import (
	"strings"
	"testing"

	"github.com/ruhorh66-rgb/air-modules/learning"
)

// This is an isolated source-only consumer upgrade. The active 0.11.9 binary
// remains pinned to the released module v0.1.0 until its own release cutover.
// A new module's strict wire version must not be silently hidden by 0.11.9.
func TestN042VersionedConsumerPinsPublishedLearning011(t *testing.T) {
	if version != "0.11.11-rc.2" {
		t.Fatalf("consumer candidate version = %q, want 0.11.11-rc.2", version)
	}
	if learning.Version != "0.1.1" {
		t.Fatalf("module dependency version = %q, want published v0.1.1", learning.Version)
	}
	out, rc := captureStdout(t, func() int { return run([]string{"version"}) })
	if rc != 0 || strings.TrimSpace(out) != "air-worker 0.11.11-rc.2" {
		t.Fatalf("version CLI mismatch (code %d): %q", rc, out)
	}
}
