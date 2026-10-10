//go:build !windows

package main

import "errors"

// The current AirWorker host contract uses Windows named mutexes.
// Non-Windows release code must not fake a proof-bearing lock.
func acquireNativeFeedbackUseLock(string) (*osLock, bool, error) {
	return nil, false, errors.New("native feedback proof requires the supported Windows mutex implementation")
}
