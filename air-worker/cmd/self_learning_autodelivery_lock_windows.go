//go:build windows

package main

// The legacy general lock helper can return (&osLock{}, true) after a
// CreateMutexW failure. A signed SessionStart delivery MUST reject that
// sentinel instead of silently losing cross-process exclusivity.
func validAutoSelfDeliveryLock(lock *osLock) bool {
	return lock != nil && lock.h != 0
}
