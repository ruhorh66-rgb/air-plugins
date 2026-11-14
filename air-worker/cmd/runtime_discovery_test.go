package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeDiscovery(t *testing.T) {
	root := t.TempDir()
	write := func(path string, v any) {
		t.Helper()
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(path, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	manifest := func(id string) runtimeManifest {
		m := runtimeManifest{SchemaVersion: 1, ProductID: "air-worker", CapabilityID: "legacy-learning", InterfaceContract: "air-worker.legacy-learning/v1", ReleaseID: id, SourceRevision: strings.Repeat("a", 40), OwnerAccepted: true, Consumers: map[string]string{"air-curator": "accepted"}, Operations: []string{"check", "event", "review", "pending-read"}}
		m.Artifact.Path = "air-worker.exe"
		m.Artifact.SHA256 = runtimeDigest([]byte(id))
		return m
	}
	for _, id := range []string{"A", "B"} {
		p := filepath.Join(root, "releases", id)
		if e := os.MkdirAll(p, 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(filepath.Join(p, "air-worker.exe"), []byte(id), 0600); e != nil {
			t.Fatal(e)
		}
		write(filepath.Join(p, "manifest.json"), manifest(id))
	}
	promote := func(id, previous string) {
		t.Helper()
		b, e := os.ReadFile(filepath.Join(root, "releases", id, "manifest.json"))
		if e != nil {
			t.Fatal(e)
		}
		write(filepath.Join(root, "production.json"), runtimePointer{1, "air-worker", id, runtimeDigest(b), true, previous})
	}
	resolve := func() (runtimeResolution, error) {
		return resolveRuntime(root, "air-curator", "legacy-learning", "air-worker.legacy-learning/v1")
	}
	for _, id := range []string{"A", "B", "A"} {
		prev := "B"
		if id == "B" {
			prev = "A"
		}
		promote(id, prev)
		r, e := resolve()
		if e != nil || r.ReleaseID != id || r.Status != "RESOLVED" {
			t.Fatalf("upgrade/rollback %s: %+v %v", id, r, e)
		}
	}
	t.Run("previous missing", func(t *testing.T) {
		promote("A", "missing")
		if _, e := resolve(); e == nil {
			t.Fatal("accepted missing previous slot")
		}
	})
	for _, id := range []string{"a", "A.", "CON", "con.txt", "LPT1", "COM9.exe"} {
		t.Run("previous alias or device "+id, func(t *testing.T) {
			promote("A", id)
			if _, e := resolve(); e == nil {
				t.Fatal("accepted aliased or reserved previous slot")
			}
		})
	}
	t.Run("previous missing artifact", func(t *testing.T) {
		path := filepath.Join(root, "releases", "B", "air-worker.exe")
		if e := os.Remove(path); e != nil {
			t.Fatal(e)
		}
		defer func() {
			if e := os.WriteFile(path, []byte("B"), 0600); e != nil {
				t.Fatal(e)
			}
		}()
		promote("A", "B")
		if _, e := resolve(); e == nil {
			t.Fatal("accepted incomplete rollback")
		}
	})
	t.Run("same physical artifact", func(t *testing.T) {
		path := filepath.Join(root, "releases", "B", "air-worker.exe")
		if e := os.Remove(path); e != nil {
			t.Fatal(e)
		}
		defer func() {
			_ = os.Remove(path)
			if e := os.WriteFile(path, []byte("B"), 0600); e != nil {
				t.Fatal(e)
			}
			write(filepath.Join(root, "releases", "B", "manifest.json"), manifest("B"))
		}()
		if e := os.Link(filepath.Join(root, "releases", "A", "air-worker.exe"), path); e != nil {
			t.Skipf("hardlink unavailable: %v", e)
		}
		m := manifest("B")
		m.Artifact.SHA256 = runtimeDigest([]byte("A"))
		write(filepath.Join(root, "releases", "B", "manifest.json"), m)
		promote("A", "B")
		if _, e := resolve(); e == nil {
			t.Fatal("accepted physically identical artifact")
		}
	})
	tests := []struct {
		name   string
		mutate func(*runtimeManifest)
	}{
		{"breaking contract", func(m *runtimeManifest) { m.InterfaceContract = "v2" }},
		{"wrong product", func(m *runtimeManifest) { m.ProductID = "other" }},
		{"wrong capability", func(m *runtimeManifest) { m.CapabilityID = "shared-learning" }},
		{"owner unaccepted", func(m *runtimeManifest) { m.OwnerAccepted = false }},
		{"consumer unaccepted", func(m *runtimeManifest) { m.Consumers["air-curator"] = "pending" }},
		{"artifact tamper", func(m *runtimeManifest) { m.Artifact.SHA256 = strings.Repeat("0", 64) }},
		{"path traversal", func(m *runtimeManifest) { m.Artifact.Path = "../air-worker.exe" }},
		{"absolute path", func(m *runtimeManifest) { m.Artifact.Path = filepath.Join(root, "air-worker.exe") }},
		{"extra operation", func(m *runtimeManifest) { m.Operations = append(m.Operations, "apply") }},
		{"missing operation", func(m *runtimeManifest) { m.Operations = m.Operations[:2] }},
		{"duplicate operation", func(m *runtimeManifest) { m.Operations = append(m.Operations, "check") }},
		{"missing provenance", func(m *runtimeManifest) { m.SourceRevision = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := manifest("A")
			tt.mutate(&m)
			write(filepath.Join(root, "releases", "A", "manifest.json"), m)
			promote("A", "B")
			if _, e := resolve(); e == nil {
				t.Fatal("accepted invalid manifest")
			}
		})
	}
	write(filepath.Join(root, "releases", "A", "manifest.json"), manifest("A"))
	for _, tt := range tests {
		t.Run("previous "+tt.name, func(t *testing.T) {
			m := manifest("B")
			tt.mutate(&m)
			write(filepath.Join(root, "releases", "B", "manifest.json"), m)
			promote("A", "B")
			if _, e := resolve(); e == nil {
				t.Fatal("accepted invalid previous manifest")
			}
		})
	}
	write(filepath.Join(root, "releases", "B", "manifest.json"), manifest("B"))
	promote("A", "B")
	t.Run("manifest hash mismatch", func(t *testing.T) {
		write(filepath.Join(root, "releases", "A", "manifest.json"), manifest("B"))
		if _, e := resolve(); e == nil {
			t.Fatal("accepted tamper")
		}
	})
	write(filepath.Join(root, "releases", "A", "manifest.json"), manifest("A"))
	promote("A", "B")
	t.Run("candidate not production", func(t *testing.T) {
		p := filepath.Join(root, "production.json")
		b, _ := os.ReadFile(p)
		var v runtimePointer
		_ = json.Unmarshal(b, &v)
		v.ProductionPromoted = false
		write(p, v)
		if _, e := resolve(); e == nil {
			t.Fatal("accepted candidate")
		}
	})
	promote("A", "B")
	t.Run("wrong requested contract", func(t *testing.T) {
		if _, e := resolveRuntime(root, "air-curator", "legacy-learning", "v2"); e == nil {
			t.Fatal("accepted unknown")
		}
	})
	t.Run("missing pointer", func(t *testing.T) {
		if e := os.Rename(filepath.Join(root, "production.json"), filepath.Join(root, "candidate.json")); e != nil {
			t.Fatal(e)
		}
		if _, e := resolve(); e == nil {
			t.Fatal("candidate fallback")
		}
	})
	t.Run("symlink", func(t *testing.T) {
		link := filepath.Join(root, "linked")
		if e := os.Symlink(filepath.Join(root, "releases"), link); e != nil {
			t.Skipf("symlink creation unavailable: %v", e)
		}
		if _, e := runtimePath(root, "linked", "A"); e == nil {
			t.Fatal("accepted symlink")
		}
	})
}
func TestRuntimeJSON(t *testing.T) {
	for _, s := range []string{`{"schema_version":1,"schema_version":2}`, `{"Schema_version":1}`, `{"schema_version":1} {}`, `{"unknown":true}`} {
		var p runtimePointer
		if e := runtimeJSON([]byte(s), &p); e == nil {
			t.Fatalf("accepted %s", s)
		}
	}
}
func TestRuntimeComponent(t *testing.T) {
	for _, part := range []string{"A.", "a..", ".", "..", "CON", "con.exe", "PRN", "AUX.log", "NUL", "COM1", "com9.exe", "LPT1", "lpt9.txt", "A ", "A:B", "a/b", "a\\b"} {
		if runtimeComponent(part) {
			t.Errorf("accepted unsafe component %q", part)
		}
	}
	for _, part := range []string{"A", "B", "production.json", "air-worker.exe", "rc.6", "COM10", "CONSOLE"} {
		if !runtimeComponent(part) {
			t.Errorf("rejected valid component %q", part)
		}
	}
}
