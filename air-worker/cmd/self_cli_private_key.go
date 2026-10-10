package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const ownCLIKeyLength = 32
const ownCLIKeyFileName = ".cli-failure-hmac-key"

func ownCLIPrivateKeyPath(runtimeRoot string) string {
	return filepath.Join(runtimeRoot, ownCLIKeyFileName)
}

// Caller owns the self-cli-failure mutex for the entire creation/readback.
// The key is installed once, in a separate protected file, and is never
// stored in CLI failure receipts or in the Git-managed learning directory.
func ownCLIPrivateKey(runtimeRoot string) ([]byte, error) {
	path := ownCLIPrivateKeyPath(runtimeRoot)
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("own CLI private key is not an ordinary file")
		}
		if err := validatePrivateCLIKeyPath(path); err != nil {
			return nil, err
		}
		sealed, err := readLearningBounded(path, 16*1024)
		if err != nil {
			return nil, err
		}
		key, err := unsealOwnCLIKey(sealed)
		if err != nil || len(key) != ownCLIKeyLength {
			return nil, errors.New("own CLI private key is unreadable or invalid; fail closed")
		}
		return key, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	// Never silently rotate a deleted key if immutable receipts already
	// exist. Re-keying would let the same run_id have two incompatible proofs.
	if entries, err := os.ReadDir(filepath.Join(runtimeRoot, "cli-failures")); err == nil {
		for _, entry := range entries {
			if strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
				return nil, errors.New("own CLI private key missing with prior receipts; recovery required")
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(runtimeRoot, 0700); err != nil {
		return nil, err
	}
	key := make([]byte, ownCLIKeyLength)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	sealed, err := sealOwnCLIKey(key)
	if err != nil {
		return nil, err
	}
	if len(sealed) < ownCLIKeyLength || len(sealed) > 16*1024 {
		return nil, errors.New("own CLI key envelope outside bounds")
	}

	// Publish without replacing any other instance's key. os.Link is
	// atomic on the supported NTFS volume and refuses an existing target.
	f, err := os.CreateTemp(runtimeRoot, ".cli-key-*.tmp")
	if err != nil {
		return nil, err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.Write(sealed); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	if err := os.Link(name, path); err != nil {
		return nil, fmt.Errorf("own CLI key publication conflict: %w", err)
	}
	if err := validatePrivateCLIKeyPath(path); err != nil {
		return nil, err
	}
	readback, err := readLearningBounded(path, 16*1024)
	if err != nil {
		return nil, err
	}
	actual, err := unsealOwnCLIKey(readback)
	same := err == nil && hmac.Equal(actual, key)
	for i := range actual {
		actual[i] = 0
	}
	if !same {
		return nil, errors.New("own CLI private key readback mismatch")
	}
	return key, nil
}

func ownCLIInvocationMAC(key []byte, argv []string) string {
	framed, _ := json.Marshal(argv)
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte("air-worker.cli.argv-hmac/v2\x00"))
	_, _ = h.Write(framed)
	return hex.EncodeToString(h.Sum(nil))
}
