package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// This owner-side resolver reads only a separately promoted deployment receipt.
// It never accepts an update feed, candidate lock, development checkout, or latest.
// Installer ownership/integrity of the bootstrap and root remains a release gate.
type runtimePointer struct {
	SchemaVersion      int    `json:"schema_version"`
	ProductID          string `json:"product_id"`
	ReleaseID          string `json:"release_id"`
	ManifestSHA256     string `json:"manifest_sha256"`
	ProductionPromoted bool   `json:"production_promoted"`
	PreviousReleaseID  string `json:"previous_release_id"`
}
type runtimeManifest struct {
	SchemaVersion     int    `json:"schema_version"`
	ProductID         string `json:"product_id"`
	CapabilityID      string `json:"capability_id"`
	InterfaceContract string `json:"interface_contract"`
	ReleaseID         string `json:"release_id"`
	SourceRevision    string `json:"source_revision"`
	Artifact          struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"artifact"`
	OwnerAccepted bool              `json:"owner_accepted"`
	Consumers     map[string]string `json:"consumers"`
	Operations    []string          `json:"operations"`
}
type runtimeResolution struct {
	Schema             string   `json:"schema"`
	Status             string   `json:"status"`
	ProductID          string   `json:"product_id"`
	CapabilityID       string   `json:"capability_id"`
	InterfaceContract  string   `json:"interface_contract"`
	Consumer           string   `json:"consumer"`
	Executable         string   `json:"executable"`
	SHA256             string   `json:"sha256"`
	SourceRevision     string   `json:"source_revision"`
	ReleaseID          string   `json:"release_id"`
	PointerSHA256      string   `json:"pointer_sha256"`
	ManifestSHA256     string   `json:"manifest_sha256"`
	PreviousReleaseID  string   `json:"previous_release_id"`
	Operations         []string `json:"operations"`
	ProductionPromoted bool     `json:"production_promoted"`
	OwnerAccepted      bool     `json:"owner_accepted"`
	ConsumerStatus     string   `json:"consumer_status"`
}

var runtimeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,95}$`)
var runtimeHash = regexp.MustCompile(`^[a-f0-9]{64}$`)
var runtimeRevision = regexp.MustCompile(`^[a-f0-9]{40}$`)

func runtimeDigest(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// Reject duplicate/case-aliased JSON keys instead of Go's last-key-wins behavior.
func runtimeJSON(data []byte, dst any) error {
	var walk func(*json.Decoder) error
	walk = func(d *json.Decoder) error {
		t, e := d.Token()
		if e != nil {
			return e
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		if delim == '{' {
			seen := map[string]bool{}
			for d.More() {
				t, e = d.Token()
				if e != nil {
					return e
				}
				k, ok := t.(string)
				if !ok {
					return errors.New("bad key")
				}
				if seen[k] {
					return errors.New("duplicate key")
				}
				seen[k] = true
				if e = walk(d); e != nil {
					return e
				}
			}
		} else if delim == '[' {
			for d.More() {
				if e = walk(d); e != nil {
					return e
				}
			}
		} else {
			return errors.New("bad delimiter")
		}
		_, e = d.Token()
		return e
	}
	d := json.NewDecoder(bytes.NewReader(data))
	if e := walk(d); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return errors.New("trailing JSON")
	}
	// Exact key spelling is checked against re-encoding; whitespace/order are irrelevant.
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if e := d.Decode(dst); e != nil {
		return e
	}
	canonical, e := json.Marshal(dst)
	if e != nil {
		return e
	}
	var original, normalized any
	if e = json.Unmarshal(data, &original); e != nil {
		return e
	}
	if e = json.Unmarshal(canonical, &normalized); e != nil {
		return e
	}
	var exactKeys func(any, any) bool
	exactKeys = func(a, b any) bool {
		am, ok := a.(map[string]any)
		if !ok {
			return true
		}
		bm, ok := b.(map[string]any)
		if !ok {
			return false
		}
		for k, v := range am {
			w, ok := bm[k]
			if !ok || !exactKeys(v, w) {
				return false
			}
		}
		return true
	}
	if !exactKeys(original, normalized) {
		return errors.New("noncanonical JSON key")
	}
	return nil
}

// Reject Windows aliases on every host, including reserved device basenames.
func runtimeComponent(part string) bool {
	if !runtimeID.MatchString(part) || strings.HasSuffix(part, ".") {
		return false
	}
	base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return false
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
		return false
	}
	return true
}

func runtimePath(root string, parts ...string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", errors.New("runtime root must be absolute")
	}
	p := filepath.Clean(root)
	// Check the root and every ancestor, not only the leaf.
	for q := p; ; q = filepath.Dir(q) {
		s, e := os.Lstat(q)
		if e != nil {
			return "", e
		}
		if s.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("symlink runtime root")
		}
		if filepath.Dir(q) == q {
			break
		}
	}
	for _, part := range parts {
		if !runtimeComponent(part) {
			return "", errors.New("invalid path component")
		}
		p = filepath.Join(p, part)
		s, e := os.Lstat(p)
		if e != nil {
			return "", e
		}
		if s.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("symlink runtime target")
		}
	}
	return p, nil
}
func runtimeRead(path string) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	s, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if !s.Mode().IsRegular() || s.Size() > 1<<20 {
		return nil, errors.New("invalid receipt file")
	}
	return io.ReadAll(io.LimitReader(f, 1<<20))
}

// Both current and rollback slots must satisfy the same accepted contract.
// The prior manifest is trusted through the immutable, owner-controlled root.
func runtimeSlot(root, releaseID, expectedHash, consumer, capability, contract string) (m runtimeManifest, manifest []byte, executable string, err error) {
	mp, e := runtimePath(root, "releases", releaseID, "manifest.json")
	if e != nil {
		return m, nil, "", e
	}
	mb, e := runtimeRead(mp)
	if e != nil {
		return m, nil, "", e
	}
	if expectedHash != "" && runtimeDigest(mb) != expectedHash {
		return m, nil, "", errors.New("manifest hash mismatch")
	}
	if e = runtimeJSON(mb, &m); e != nil {
		return m, nil, "", e
	}
	if m.SchemaVersion != 1 || m.ProductID != "air-worker" || m.ReleaseID != releaseID || m.CapabilityID != capability || m.InterfaceContract != contract || !m.OwnerAccepted || m.Consumers[consumer] != "accepted" || !runtimeRevision.MatchString(m.SourceRevision) || !runtimeHash.MatchString(m.Artifact.SHA256) {
		return m, nil, "", errors.New("contract or acceptance mismatch")
	}
	required := map[string]bool{"check": false, "event": false, "review": false, "pending-read": false}
	for _, op := range m.Operations {
		if _, ok := required[op]; !ok || required[op] {
			return m, nil, "", errors.New("unsupported or duplicate operation")
		}
		required[op] = true
	}
	for _, present := range required {
		if !present {
			return m, nil, "", errors.New("missing operation")
		}
	}
	// Artifact is a single basename inside the accepted immutable release slot.
	executable, e = runtimePath(root, "releases", releaseID, m.Artifact.Path)
	if e != nil {
		return m, nil, "", e
	}
	f, e := os.Open(executable)
	if e != nil {
		return m, nil, "", e
	}
	defer f.Close()
	s, e := f.Stat()
	if e != nil {
		return m, nil, "", e
	}
	if !s.Mode().IsRegular() {
		return m, nil, "", errors.New("artifact not regular")
	}
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return m, nil, "", e
	}
	if hex.EncodeToString(h.Sum(nil)) != m.Artifact.SHA256 {
		return m, nil, "", errors.New("artifact hash mismatch")
	}

	return m, mb, executable, nil
}

func resolveRuntime(root, consumer, capability, contract string) (runtimeResolution, error) {
	var out runtimeResolution
	if consumer != "air-curator" || capability != "legacy-learning" || contract != "air-worker.legacy-learning/v1" {
		return out, errors.New("unsupported consumer/capability/contract")
	}
	pp, e := runtimePath(root, "production.json")
	if e != nil {
		return out, e
	}
	pb, e := runtimeRead(pp)
	if e != nil {
		return out, e
	}
	var p runtimePointer
	if e = runtimeJSON(pb, &p); e != nil {
		return out, e
	}
	if p.SchemaVersion != 1 || p.ProductID != "air-worker" || !p.ProductionPromoted || !runtimeHash.MatchString(p.ManifestSHA256) || !runtimeComponent(p.ReleaseID) || !runtimeComponent(p.PreviousReleaseID) || strings.EqualFold(p.PreviousReleaseID, p.ReleaseID) {
		return out, errors.New("unpromoted or invalid production pointer")
	}
	m, mb, executable, e := runtimeSlot(root, p.ReleaseID, p.ManifestSHA256, consumer, capability, contract)
	if e != nil {
		return out, e
	}
	_, _, previousExecutable, e := runtimeSlot(root, p.PreviousReleaseID, "", consumer, capability, contract)
	if e != nil {
		return out, fmt.Errorf("invalid previous release: %w", e)
	}
	// String inequality alone is insufficient on case-insensitive filesystems.
	for _, pair := range [][2]string{
		{filepath.Dir(executable), filepath.Dir(previousExecutable)},
		{executable, previousExecutable},
	} {
		current, err := os.Stat(pair[0])
		if err != nil {
			return out, err
		}
		previous, err := os.Stat(pair[1])
		if err != nil {
			return out, err
		}
		if os.SameFile(current, previous) {
			return out, errors.New("current and previous release alias the same physical target")
		}
	}
	again, e := runtimeRead(pp)
	if e != nil {
		return out, e
	}
	if !bytes.Equal(pb, again) {
		return out, errors.New("UNKNOWN: pointer changed during resolution")
	}
	return runtimeResolution{"air-worker.runtime-resolution/v1", "RESOLVED", m.ProductID, m.CapabilityID, m.InterfaceContract, consumer, executable, m.Artifact.SHA256, m.SourceRevision, m.ReleaseID, runtimeDigest(pb), runtimeDigest(mb), p.PreviousReleaseID, m.Operations, true, true, "accepted"}, nil
}
func cmdRuntime(args []string) int {
	if len(args) == 0 || args[0] != "resolve" {
		fmt.Fprintln(os.Stderr, "runtime resolve required")
		return 2
	}
	fs := flag.NewFlagSet("runtime resolve", flag.ContinueOnError)
	consumer := fs.String("consumer", "", "")
	capability := fs.String("capability", "", "")
	contract := fs.String("contract", "", "")
	asJSON := fs.Bool("json", false, "")
	root := fs.String("root", "", "explicit isolated fixture root; never supplied by production adapter")
	if fs.Parse(args[1:]) != nil || fs.NArg() != 0 || !*asJSON {
		return 2
	}
	if *root == "" {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" || !filepath.IsAbs(base) {
			fmt.Fprintln(os.Stderr, "LOCALAPPDATA unavailable")
			return 2
		}
		*root = filepath.Join(base, "air-worker", "runtime")
	}
	result, e := resolveRuntime(*root, *consumer, *capability, *contract)
	if e != nil {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"schema": "air-worker.runtime-resolution/v1", "status": "UNRESOLVED", "error": e.Error()})
		return 2
	}
	if json.NewEncoder(os.Stdout).Encode(result) != nil {
		return 2
	}
	return 0
}
