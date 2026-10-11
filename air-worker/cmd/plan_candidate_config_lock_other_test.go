//go:build !windows

package main

import "testing"

func TestPlanCandidateConfigLockUnavailable(t *testing.T) {
	if lock, err := lockCandidateConfig(t.TempDir()); err == nil || lock != nil {
		t.Fatal("source config fence must fail closed outside Windows")
	}
}
