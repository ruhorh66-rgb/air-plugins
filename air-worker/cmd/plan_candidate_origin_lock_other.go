//go:build !windows

package main

import "fmt"

type candidateOriginLock struct{}

func lockCandidateOrigin(string) (*candidateOriginLock, error) {
	return nil, fmt.Errorf("reliable origin replacement fence unavailable on this OS")
}
func (*candidateOriginLock) Verify() error { return fmt.Errorf("origin fence unavailable") }
func (*candidateOriginLock) Close()        {}
