//go:build !windows

package main

import "os"

type nativeInputIdentity struct{ Info os.FileInfo }

func nativeInputPathIdentity(path string) (nativeInputIdentity, error) {
	stat, err := os.Stat(path)
	return nativeInputIdentity{Info: stat}, err
}

func nativeInputOpenedIdentity(file *os.File) (nativeInputIdentity, error) {
	stat, err := file.Stat()
	return nativeInputIdentity{Info: stat}, err
}

func nativeInputSameIdentity(a, b nativeInputIdentity) bool {
	return a.Info != nil && b.Info != nil && os.SameFile(a.Info, b.Info)
}
