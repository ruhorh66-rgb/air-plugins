//go:build !windows

package main

import (
	"errors"
	"os"
)

// Non-Windows development fixtures use a separate owner-only key file.
// Only Windows DPAPI is used for production SRVLM01 installations.
func sealOwnCLIKey(key []byte) ([]byte, error)    { return append([]byte(nil), key...), nil }
func unsealOwnCLIKey(data []byte) ([]byte, error) { return append([]byte(nil), data...), nil }

func validatePrivateCLIKeyPath(path string) error {
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if st.Mode().Perm()&0077 != 0 {
		return errors.New("own CLI private key has nonprivate file permissions")
	}
	return nil
}
