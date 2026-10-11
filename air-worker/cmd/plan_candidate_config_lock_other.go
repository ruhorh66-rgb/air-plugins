//go:build !windows

package main

import "fmt"

func lockCandidateConfig(string) (*candidateOriginLock, error) {
	return nil, fmt.Errorf("source config fence requires Windows")
}
