package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionCommandMatchesReleaseTarget(t *testing.T) {
	out, code := captureStdout(t, func() int { return run([]string{"version"}) })
	if code != 0 {
		t.Fatalf("version exit code=%d, want 0", code)
	}
	if got, want := strings.TrimSpace(out), "air-worker 1.0.0-rc.2"; got != want {
		t.Fatalf("version output=%q, want %q", got, want)
	}
}

func TestReleaseMetadataMatches100RC1(t *testing.T) {
	if version != "1.0.0-rc.2" {
		t.Fatalf("unexpected release target %q", version)
	}
	for _, name := range []string{".claude-plugin", ".codex-plugin"} {
		path := filepath.Join("..", name, "plugin.json")
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		var m struct {
			Version string `json:"version"`
		}
		if e = json.Unmarshal(b, &m); e != nil {
			t.Fatal(e)
		}
		if m.Version != version {
			t.Fatalf("%s version=%s want=%s", path, m.Version, version)
		}
	}
	b, e := os.ReadFile(filepath.Join("..", ".hermes", "plugins", "air-worker", "plugin.yaml"))
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(strings.ReplaceAll(string(b), "\r\n", "\n"), "\nversion: "+version+"\n") {
		t.Fatal("Hermes plugin release version mismatch")
	}
	for _, profile := range []string{"airworker-hermes-v1-enforce", "airworker-hermes-v1-shadow"} {
		path := filepath.Join("..", ".hermes", "profiles", profile, "profile-contract.json")
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		var m struct {
			Plugin struct {
				Version string `json:"version"`
			} `json:"plugin"`
		}
		if e = json.Unmarshal(b, &m); e != nil {
			t.Fatal(e)
		}
		if m.Plugin.Version != version {
			t.Fatalf("%s version=%s want=%s", path, m.Plugin.Version, version)
		}
	}
}
