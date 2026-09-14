package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// К7 этапа 0.10: статус значка называет причину и путь, а не гадает «старая версия трея?».
func TestДоказательствоЗначкаНазываетПричину(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "нет.json")
	empty := filepath.Join(dir, "пусто.json")
	broken := filepath.Join(dir, "битый.json")
	withBOM := filepath.Join(dir, "bom.json")
	for path, body := range map[string][]byte{
		empty:   {},
		broken:  []byte("{pid:"),
		withBOM: append(append([]byte{}, utf8BOM...), []byte(`{"pid":7,"shell_accepted":true}`)...),
	} {
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	kinds := map[string]bool{}
	for _, p := range []string{missing, empty, broken} {
		proof, why := trayProofFrom(p)
		if proof != nil || why == "" || !strings.Contains(why, p) {
			t.Errorf("%s: доказательство %v, причина «%s» — ожидались nil и причина с путём", p, proof, why)
			continue
		}
		kinds[why[:strings.Index(why, ":")]] = true
	}
	if len(kinds) != 3 {
		t.Errorf("три разные беды дали %d разных причин: %v", len(kinds), kinds)
	}

	if proof, why := trayProofFrom(withBOM); proof == nil || !proof.Accepted || proof.PID != 7 || why != "" {
		t.Errorf("файл с BOM не прочитан: %v, «%s»", proof, why)
	}
}
