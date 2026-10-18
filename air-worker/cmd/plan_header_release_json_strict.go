package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	planHeaderReleaseSidecarLimit   = 1 << 20
	planHeaderReleaseJSONDepthLimit = 64
)

// planHeaderReadReleaseSidecarBounded reads release facts for DEV-136's
// informational preflight. Never use an unbounded os.ReadFile after stat:
// the file might grow between those operations. The opened file is bounded
// and its identity, size, and mtime are verified before and after reading.
// This is NOT a native generation lock or an authorization to mutate PLAN.
func planHeaderReadReleaseSidecarBounded(path string) ([]byte, error) {
	if err := planHeaderRejectOSReparse(path); err != nil {
		return nil, err
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() ||
		before.Size() < 0 || before.Size() > planHeaderReleaseSidecarLimit {
		return nil, errors.New("machine release-slices unavailable, nonregular or oversized")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) ||
		opened.Size() < 0 || opened.Size() > planHeaderReleaseSidecarLimit {
		_ = f.Close()
		return nil, errors.New("machine release-slices changed identity or size on open")
	}
	if err := planHeaderVerifyReleaseHandle(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(f, planHeaderReleaseSidecarLimit+1))
	afterOpen, statErr := f.Stat()
	handleErr := planHeaderVerifyReleaseHandle(f)
	closeErr := f.Close()
	if readErr != nil || statErr != nil || handleErr != nil || closeErr != nil ||
		len(raw) > planHeaderReleaseSidecarLimit ||
		!os.SameFile(opened, afterOpen) || opened.Size() != afterOpen.Size() ||
		!opened.ModTime().Equal(afterOpen.ModTime()) {
		return nil, errors.New("machine release-slices changed or exceeded bounded read")
	}
	if err := planHeaderRejectOSReparse(path); err != nil {
		return nil, err
	}
	latest, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, latest) ||
		latest.Size() != opened.Size() || !latest.ModTime().Equal(opened.ModTime()) {
		return nil, errors.New("machine release-slices path changed during preflight")
	}
	if !utf8.Valid(raw) {
		return nil, errors.New("machine release-slices invalid UTF-8")
	}
	return raw, nil
}

// planHeaderJSONKeyFold mirrors encoding/json's foldName for matching Go
// struct fields: uppercase ASCII bytes plus Unicode SimpleFold's minimum
// code point per equivalence class. strings.ToLower is insufficient: the
// long-s rune (U+017F) folds to ASCII 'S' although ToLower preserves it.
// Incoming JSON has already been validated as UTF-8.
func planHeaderJSONKeyFold(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < utf8.RuneSelf {
			if 'a' <= r && r <= 'z' {
				r -= 'a' - 'A'
			}
		} else {
			for {
				next := unicode.SimpleFold(r)
				if next <= r {
					r = next
					break
				}
				r = next
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Go's encoding/json.Unmarshal accepts repeated keys and overwrites earlier
// values. For a machine authority fact that ambiguity is unacceptable, even
// inside an otherwise ignored nested object. Case-folded duplicates are also
// rejected because Go's struct field matching may be case insensitive.
// Preserve extensibility: unknown UNIQUE JSON keys remain allowed.
func planHeaderRejectAmbiguousJSON(raw []byte) error {
	if !utf8.Valid(raw) {
		return errors.New("release sidecar contains invalid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	var scan func(int) error
	scan = func(depth int) error {
		if depth > planHeaderReleaseJSONDepthLimit {
			return errors.New("release sidecar JSON depth limit exceeded")
		}
		token, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := make(map[string]bool)
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return errors.New("release sidecar object key not a string")
				}
				folded := planHeaderJSONKeyFold(key)
				if seen[folded] {
					return fmt.Errorf("duplicate machine release JSON key %q", key)
				}
				seen[folded] = true
				if err := scan(depth + 1); err != nil {
					return err
				}
			}
			end, err := dec.Token()
			if err != nil || end != json.Delim('}') {
				return errors.New("machine release JSON object not closed")
			}
		case '[':
			for dec.More() {
				if err := scan(depth + 1); err != nil {
					return err
				}
			}
			end, err := dec.Token()
			if err != nil || end != json.Delim(']') {
				return errors.New("machine release JSON array not closed")
			}
		default:
			return errors.New("machine release JSON invalid delimiter")
		}
		return nil
	}
	if err := scan(0); err != nil {
		return fmt.Errorf("machine release JSON ambiguous or invalid: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("machine release JSON contains trailing or invalid tokens")
	}
	return nil
}
