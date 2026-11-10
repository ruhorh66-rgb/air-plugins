package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const n124NodePath = "plan/N-124_p1-detect-cross-branch-node-id-collision-and-sta.md"
const n124ReceiptPath = "docs/receipts/AIRWORKER_N124_CANCELLED_BY_LPR_20261110.md"

// The original slice from the pre-cancellation release plan.
const n124HistoricalSlice = `{"version":"0.11.24","status":"planned","feature":"Глобально безопасные ID и PLAN по нескольким веткам","owner":"AirWorker native executor ladder / SRVLM01","trigger":"Решение ЛПР 12.10.2026: выпускать небольшими релизами, первым — реальное самообучение AirWorker; другое позже.","done_when":"Native plan node new/close в двух реальных конкурирующих тестовых ветках без коллизий и с каноническим authority + outbox; успешно создаёт N121 и закрывает принятый узел только после factual PASS.","judge":"AirWorker factual exit+receipt; independent Codex gpt-6-sol:low read-only","receipt":"docs/receipts/RELEASE_0_11_24_ACCEPTANCE.json","rollback_to":"0.11.23","dependencies":["0.11.23"],"node_refs":["N-124"],"deferred":["Незавершённый A3-1 scoped PASS хранится отдельной квитанцией, не закрывает N-124","12.10.2026 LPR sequence: self-learning 0.11.9 -> Hermes 0.11.10-0.11.13 -> OmniRouter 0.11.14-0.11.18 -> this backlog."],"steps":[{"id":"R0.11.24-01","status":"planned","title":"Завершить A3 native new/close/outbox reconcile","owner":"AirWorker native executor ladder / SRVLM01","trigger":"Решение ЛПР 12.10.2026: первый релиз — самообучение, короткие прерываемые шаги; релиз 0.11.9.6 Sequencing LPR 12.10.2026: AFTER Hermes+OmniRouter.","done_when":"Тестовая каноническая ветка поддерживает атомарный remote-first new/close с durable replay и отсутствием локальной мутации при отказе","judge":"AirWorker factual exit+receipt; independent Codex gpt-6-sol:low read-only","receipt":"docs/receipts/RELEASE_0_11_24_01.json","budget_minutes":12,"kind":"source","node_refs":["N-124"]},{"id":"R0.11.24-02","status":"planned","title":"Реальная cross-branch конкуренция и Sol Low","owner":"AirWorker native executor ladder / SRVLM01","trigger":"Решение ЛПР 12.10.2026: первый релиз — самообучение, короткие прерываемые шаги; релиз 0.11.9.6 Sequencing LPR 12.10.2026: AFTER Hermes+OmniRouter.","done_when":"Две stale ветки: ровно один writer, новых повторных IDs нет, negative tests и Sol Low PASS","judge":"AirWorker factual exit+receipt; independent Codex gpt-6-sol:low read-only","receipt":"docs/receipts/RELEASE_0_11_24_02.json","budget_minutes":12,"kind":"judge","node_refs":["N-124"]}]}`

func TestReleaseSlicesN124Cancellation(t *testing.T) {
	cases := []struct {
		name   string
		valid  bool
		change func(string)
	}{
		{"current plan", true, nil},
		{"exact LF receipt", true, func(root string) { setN124ReceiptLineEndings(t, root, "\n") }},
		{"exact CRLF receipt", true, func(root string) { setN124ReceiptLineEndings(t, root, "\r\n") }},
		{"mixed line endings receipt", false, func(root string) {
			setN124ReceiptLineEndings(t, root, "\n")
			replaceN124File(t, root, n124ReceiptPath, "\n", "\r\n")
		}},
		{"historical complete train", true, func(root string) {
			editN124Releases(t, root, func(d *releaseSlicesDoc) { restoreN124(t, d) })
			removeN124File(t, root, n124ReceiptPath)
		}},
		{"missing receipt", false, func(root string) { removeN124File(t, root, n124ReceiptPath) }},
		{"altered receipt", false, func(root string) { replaceN124File(t, root, n124ReceiptPath, "CANCELLED_BY_LPR", "NOT_CANCELLED") }},
		{"missing node", false, func(root string) { removeN124File(t, root, n124NodePath) }},
		{"node not closed", false, func(root string) { replaceN124File(t, root, n124NodePath, `status: "closed"`, `status: "open"`) }},
		{"different missing version", false, func(root string) {
			editN124Releases(t, root, func(d *releaseSlicesDoc) {
				restoreN124(t, d)
				for i := range d.Releases {
					if d.Releases[i].Version == "0.11.24" {
						d.Releases[i].Dependencies = []string{"0.11.22"}
						d.Releases[i].RollbackTo = "0.11.22"
					}
				}
				for i, r := range d.Releases {
					if r.Version == "0.11.23" {
						d.Releases = append(d.Releases[:i], d.Releases[i+1:]...)
						return
					}
				}
				t.Fatal("missing 0.11.23")
			})
		}},
		{"extra version", false, func(root string) {
			editN124Releases(t, root, func(d *releaseSlicesDoc) {
				extra := d.Releases[len(d.Releases)-1]
				extra.Version = "0.11.27"
				d.Releases = append(d.Releases, extra)
			})
		}},
		{"duplicate version", false, func(root string) {
			editN124Releases(t, root, func(d *releaseSlicesDoc) {
				d.Releases[len(d.Releases)-1].Version = d.Releases[len(d.Releases)-2].Version
			})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for _, name := range []string{"plan/release-slices.json", n124NodePath, n124ReceiptPath} {
				raw, err := os.ReadFile(filepath.Join("..", filepath.FromSlash(name)))
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(root, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.change != nil {
				tc.change(root)
			}
			state := inspectReleaseSlices(root)
			if !state.Found || (len(state.Problems) == 0) != tc.valid {
				t.Fatalf("found=%v problems=%+v; want valid=%v", state.Found, state.Problems, tc.valid)
			}
		})
	}
}

func setN124ReceiptLineEndings(t *testing.T, root, ending string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(n124ReceiptPath))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lf := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(lf, "\n", ending)), 0600); err != nil {
		t.Fatal(err)
	}
}

func editN124Releases(t *testing.T, root string, edit func(*releaseSlicesDoc)) {
	t.Helper()
	path := filepath.Join(root, "plan", "release-slices.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc releaseSlicesDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	edit(&doc)
	raw, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func restoreN124(t *testing.T, doc *releaseSlicesDoc) {
	t.Helper()
	var old releaseSlice
	if err := json.Unmarshal([]byte(n124HistoricalSlice), &old); err != nil {
		t.Fatal(err)
	}
	for i := range doc.Releases {
		if doc.Releases[i].Version == "0.11.25" {
			doc.Releases[i].RollbackTo = "0.11.24"
			doc.Releases[i].Dependencies = []string{"0.11.24"}
			doc.Releases = append(doc.Releases[:i], append([]releaseSlice{old}, doc.Releases[i:]...)...)
			return
		}
	}
	t.Fatal("missing 0.11.25")
}

func removeN124File(t *testing.T, root, name string) {
	t.Helper()
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(name))); err != nil {
		t.Fatal(err)
	}
}

func replaceN124File(t *testing.T, root, name, before, after string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(raw), before, after, 1)
	if changed == string(raw) {
		t.Fatalf("missing %q", before)
	}
	if err := os.WriteFile(path, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
}
