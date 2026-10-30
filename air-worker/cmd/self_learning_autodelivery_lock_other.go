//go:build !windows

package main

// Non-Windows acquireLock is a no-op. Never claim an exclusive signed
// delivery on a platform without an OS-backed cross-process mutex.
func validAutoSelfDeliveryLock(_ *osLock) bool {
	return false
}
