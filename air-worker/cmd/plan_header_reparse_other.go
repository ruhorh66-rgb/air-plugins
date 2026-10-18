//go:build !windows

package main

// POSIX links are covered by filepath.EvalSymlinks and os.Lstat in the
// shared validator. Windows reparse-point flags do not exist on this OS.
func planHeaderRejectOSReparse(_ string) error {
	return nil
}
