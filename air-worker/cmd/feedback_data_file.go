package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// One bounded JSON message, not Windows shell quoting, transports real feedback
// to the learning reviewer. Reject duplicate keys and unknown fields before
// any native shared-learning event, review, or managed file mutation.
func readNativeFeedbackRequest(path string) (map[string]string, string, error) {
	if !filepath.IsAbs(path) {
		return nil, "", errors.New("feedback -data-file must be absolute")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, "", err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Size() < 2 || info.Size() > 64*1024 {
		return nil, "", errors.New("feedback data file must be bounded regular JSON")
	}
	if _, err := os.Readlink(path); err == nil {
		return nil, "", errors.New("feedback data file may not be a reparse/symlink")
	}
	b, err := readLearningBounded(path, 64*1024)
	if err != nil {
		return nil, "", err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	first, err := dec.Token()
	if err != nil || first != json.Delim('{') {
		return nil, "", errors.New("feedback data must contain one JSON object")
	}
	allowed := map[string]bool{
		"text": true, "kind": true, "source": true, "ref": true,
		"run_id": true, "use_procedure": true,
		"type": true, "severity": true, "source-version": true, "expected": true,
		"reproduction": true, "workaround": true, "proposed-outcome": true,
	}
	data := map[string]string{}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, "", err
		}
		key, ok := t.(string)
		if !ok || !allowed[key] {
			return nil, "", fmt.Errorf("unsupported feedback data field %q", key)
		}
		if _, exists := data[key]; exists {
			return nil, "", fmt.Errorf("duplicate feedback data field %q", key)
		}
		var val any
		if err := dec.Decode(&val); err != nil {
			return nil, "", err
		}
		value, ok := val.(string)
		if !ok {
			return nil, "", fmt.Errorf("feedback data field %q must be a string", key)
		}
		data[key] = value
	}
	end, err := dec.Token()
	if err != nil || end != json.Delim('}') {
		return nil, "", errors.New("malformed feedback JSON object")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, "", errors.New("feedback data contains trailing JSON or garbage")
	}
	if strings.TrimSpace(data["text"]) == "" || strings.TrimSpace(data["run_id"]) == "" ||
		strings.TrimSpace(data["kind"]) == "" {
		return nil, "", errors.New("feedback JSON requires nonempty text, run_id and kind")
	}
	return data, learnSHA(b), nil
}
