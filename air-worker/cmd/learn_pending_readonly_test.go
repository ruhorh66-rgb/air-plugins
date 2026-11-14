package main

import (
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func readOnlyPendingSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			out[rel] = "directory"
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = "file:" + string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func writeReadOnlyPendingRows(t *testing.T, path string, rows []learnProposal) {
	t.Helper()
	if rows == nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	b, err := marshalLearnJSONL(rows)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}
func invokeReadOnlyPending(t *testing.T, root string) (int, map[string]any) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = saved; _ = reader.Close(); _ = writer.Close() }()
	code := cmdLearnPending([]string{"-product", root, "-read-only", "-json"})
	_ = writer.Close()
	os.Stdout = saved
	raw, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if code == 0 {
		if err = json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("invalid output %q: %v", raw, err)
		}
	}
	return code, out
}
func TestLearnPendingReadOnlyProjection(t *testing.T) {
	a := learnProposal{Schema: learnSchemaVersion, ID: "LP-a", Status: learnPending, Class: "test", Rule: "first"}
	b := a
	b.ID = "LP-b"
	b.Rule = "second"
	conflict := a
	conflict.Rule = "conflicting"
	applied := a
	applied.Status = learnApplied
	for _, tt := range []struct {
		name              string
		canonical, legacy []learnProposal
		want              int
		fail              bool
	}{
		{"empty", nil, nil, 0, false},
		{"legacy-only", nil, []learnProposal{a}, 1, false},
		{"canonical-only", []learnProposal{a}, nil, 1, false},
		{"mixed", []learnProposal{a}, []learnProposal{b}, 2, false},
		{"same-duplicate", []learnProposal{a}, []learnProposal{a}, 1, false},
		{"legacy-duplicate", nil, []learnProposal{a, a}, 1, false},
		{"canonical-supersedes", []learnProposal{applied}, []learnProposal{a}, 0, false},
		{"cross-conflict", []learnProposal{a}, []learnProposal{conflict}, 0, true},
		{"canonical-conflict", []learnProposal{a, conflict}, nil, 0, true},
		{"legacy-conflict", nil, []learnProposal{a, conflict}, 0, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeReadOnlyPendingRows(t, learnPaths(root).Proposals, tt.canonical)
			writeReadOnlyPendingRows(t, filepath.Join(root, ".air-worker", "learn", "proposals.jsonl"), tt.legacy)
			before := readOnlyPendingSnapshot(t, root)
			code, out := invokeReadOnlyPending(t, root)
			if tt.fail {
				if code != 2 {
					t.Fatalf("expected refusal, code %d", code)
				}
			} else {
				if code != 0 || out["read_only"] != true || out["count"] != float64(tt.want) || out["status"] != learnPending {
					t.Fatalf("code=%d output=%+v", code, out)
				}
			}
			if after := readOnlyPendingSnapshot(t, root); !reflect.DeepEqual(before, after) {
				t.Fatalf("read-only changed files/directories: before=%v after=%v", before, after)
			}
		})
	}
}
func TestLearnPendingReadOnlyRejectsUnsafePaths(t *testing.T) {
	t.Run("nonregular ledger", func(t *testing.T) {
		root := t.TempDir()
		path := learnPaths(root).Proposals
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		before := readOnlyPendingSnapshot(t, root)
		if code, _ := invokeReadOnlyPending(t, root); code != 2 {
			t.Fatal("accepted directory ledger")
		}
		if !reflect.DeepEqual(before, readOnlyPendingSnapshot(t, root)) {
			t.Fatal("modified unsafe fixture")
		}
	})
	t.Run("junction or symlink ancestor", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		target := filepath.Join(outside, "proposals.jsonl")
		writeReadOnlyPendingRows(t, target, []learnProposal{{Schema: learnSchemaVersion, ID: "LP-outside", Status: learnPending}})
		before := readOnlyPendingSnapshot(t, outside)
		link := filepath.Join(root, "learn")
		if runtime.GOOS == "windows" {
			if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput(); err != nil {
				t.Fatalf("junction fixture: %s %v", out, err)
			}
		} else {
			if err := os.Symlink(outside, link); err != nil {
				t.Fatal(err)
			}
		}
		defer os.Remove(link)
		if code, _ := invokeReadOnlyPending(t, root); code != 2 {
			t.Fatal("accepted reparse ancestor")
		}
		if !reflect.DeepEqual(before, readOnlyPendingSnapshot(t, outside)) {
			t.Fatal("modified outside target")
		}
	})
}
func TestLearnPendingOrdinaryMigrationPreserved(t *testing.T) {
	root := t.TempDir()
	writeReadOnlyPendingRows(t, filepath.Join(root, ".air-worker", "learn", "proposals.jsonl"), []learnProposal{{Schema: learnSchemaVersion, ID: "LP-old", Status: learnPending}})
	if code := cmdLearnPending([]string{"-product", root, "-json"}); code != 0 {
		t.Fatalf("ordinary pending code=%d", code)
	}
	rows, err := readLearnProposalsNoMigration(learnPaths(root).Proposals)
	if err != nil || len(rows) != 1 || rows[0].ID != "LP-old" {
		t.Fatalf("ordinary migration changed: %+v %v", rows, err)
	}
}
