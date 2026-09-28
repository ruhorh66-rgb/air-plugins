package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const curatorDecisionSchema = "air-worker.curator-decision/v1"

type curatorDecisionRecord struct {
	Schema       string `json:"schema"`
	ID           string `json:"id"`
	At           string `json:"at"`
	Authority    string `json:"authority"`
	Executable   bool   `json:"executable"`
	Principal    string `json:"principal"`
	ProfileID    string `json:"profile_id,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	RunID        string `json:"run_id,omitempty"`
	Kind         string `json:"kind"`
	Subject      string `json:"subject"`
	Decision     string `json:"decision"`
	Text         string `json:"text,omitempty"`
	Reference    string `json:"reference,omitempty"`
	PreviousHash string `json:"previous_hash,omitempty"`
	RecordHash   string `json:"record_hash"`
}

func curatorDecisionPath(dir string) string { return filepath.Join(dir, "decisions.jsonl") }

func cleanCuratorDecisionText(name, value string, required bool) (string, error) {
	value = strings.TrimSpace(value)
	if required && value == "" {
		return "", fmt.Errorf("-%s is required", name)
	}
	if len([]rune(value)) > 4000 {
		return "", fmt.Errorf("-%s is too long", name)
	}
	return value, nil
}

func curatorDecisionHash(rec curatorDecisionRecord) string {
	copy := rec
	copy.RecordHash = ""
	raw, _ := json.Marshal(copy)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func readCuratorDecisions(path string) ([]curatorDecisionRecord, error) {
	var out []curatorDecisionRecord
	err := scanLearnJSONL(path, func(raw []byte) error {
		var rec curatorDecisionRecord
		if err := json.Unmarshal(raw, &rec); err != nil {
			return err
		}
		out = append(out, rec)
		return nil
	})
	return out, err
}

func verifyCuratorDecisions(rows []curatorDecisionRecord) error {
	prev := ""
	for i, rec := range rows {
		if rec.Schema != curatorDecisionSchema {
			return fmt.Errorf("decision[%d] schema=%q", i, rec.Schema)
		}
		if rec.Authority != "audit-only" || rec.Executable {
			return fmt.Errorf("decision[%d] has invalid authority/executable state", i)
		}
		if rec.PreviousHash != prev {
			return fmt.Errorf("decision[%d] previous_hash mismatch", i)
		}
		if want := curatorDecisionHash(rec); rec.RecordHash != want {
			return fmt.Errorf("decision[%d] record_hash mismatch", i)
		}
		prev = rec.RecordHash
	}
	return nil
}

func cmdCuratorDecision(argv []string) int {
	if len(argv) == 0 {
		fmt.Fprintln(os.Stderr, "usage: air-worker curator decision record|list|verify ...")
		return 2
	}
	switch argv[0] {
	case "record":
		return cmdCuratorDecisionRecord(argv[1:])
	case "list":
		return cmdCuratorDecisionList(argv[1:])
	case "verify":
		return cmdCuratorDecisionVerify(argv[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown curator decision action %q\n", argv[0])
		return 2
	}
}

func cmdCuratorDecisionRecord(argv []string) int {
	fs := flag.NewFlagSet("curator decision record", flag.ContinueOnError)
	principalRaw := fs.String("principal", "", "who supplied this audit note")
	profile := fs.String("profile", "", "profile id")
	session := fs.String("session", "", "session id")
	run := fs.String("run", "", "run id")
	kind := fs.String("kind", "", "decision kind")
	subject := fs.String("subject", "", "decision subject")
	decision := fs.String("decision", "", "decision value")
	textRaw := fs.String("text", "", "verbatim/audit text; never executable")
	reference := fs.String("reference", "", "external receipt/reference")
	stateDir := fs.String("state-dir", "", "override machine state dir")
	asJSON := fs.Bool("json", false, "machine output")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	principal, err := sanitizeIdentityPart("principal", *principalRaw)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	k, err := cleanCuratorDecisionText("kind", *kind, true)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	subj, err := cleanCuratorDecisionText("subject", *subject, true)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	dec, err := cleanCuratorDecisionText("decision", *decision, true)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	txt, err := cleanCuratorDecisionText("text", *textRaw, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	dir := curatorControlDir(*stateDir)
	lock, err := curatorStateLock(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer lock.release()
	rows, err := readCuratorDecisions(curatorDecisionPath(dir))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := verifyCuratorDecisions(rows); err != nil {
		fmt.Fprintln(os.Stderr, "decision journal is not trustworthy:", err)
		return 2
	}
	id, err := newCuratorID("CD")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	prev := ""
	if len(rows) > 0 {
		prev = rows[len(rows)-1].RecordHash
	}
	rec := curatorDecisionRecord{
		Schema: curatorDecisionSchema, ID: id, At: time.Now().UTC().Format(time.RFC3339Nano),
		Authority: "audit-only", Executable: false, Principal: principal,
		ProfileID: strings.TrimSpace(*profile), SessionID: strings.TrimSpace(*session), RunID: strings.TrimSpace(*run),
		Kind: k, Subject: subj, Decision: dec, Text: txt, Reference: strings.TrimSpace(*reference),
		PreviousHash: prev,
	}
	rec.RecordHash = curatorDecisionHash(rec)
	if err := appendLearnJSON(curatorDecisionPath(dir), rec); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if *asJSON {
		raw, _ := json.Marshal(rec)
		fmt.Println(string(raw))
	} else {
		fmt.Printf("curator decision %s recorded: authority=audit-only executable=false hash=%s\n", rec.ID, rec.RecordHash)
	}
	return 0
}

func cmdCuratorDecisionList(argv []string) int {
	fs := flag.NewFlagSet("curator decision list", flag.ContinueOnError)
	stateDir := fs.String("state-dir", "", "override machine state dir")
	asJSON := fs.Bool("json", false, "machine output")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	rows, err := readCuratorDecisions(curatorDecisionPath(curatorControlDir(*stateDir)))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if *asJSON {
		raw, _ := json.Marshal(map[string]any{"schema": curatorDecisionSchema, "decisions": rows})
		fmt.Println(string(raw))
		return 0
	}
	for _, rec := range rows {
		fmt.Printf("%s | %s | %s=%s | principal=%s | authority=%s | executable=%t\n",
			rec.ID, rec.Kind, rec.Subject, rec.Decision, rec.Principal, rec.Authority, rec.Executable)
	}
	return 0
}

func cmdCuratorDecisionVerify(argv []string) int {
	fs := flag.NewFlagSet("curator decision verify", flag.ContinueOnError)
	stateDir := fs.String("state-dir", "", "override machine state dir")
	asJSON := fs.Bool("json", false, "machine output")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	rows, err := readCuratorDecisions(curatorDecisionPath(curatorControlDir(*stateDir)))
	if err == nil {
		err = verifyCuratorDecisions(rows)
	}
	ok := err == nil
	if *asJSON {
		errText := ""
		if err != nil {
			errText = err.Error()
		}
		raw, _ := json.Marshal(map[string]any{"schema": curatorDecisionSchema, "ok": ok, "count": len(rows), "error": errText})
		fmt.Println(string(raw))
	} else if ok {
		fmt.Printf("curator decision journal: PASS (%d records)\n", len(rows))
	} else {
		fmt.Fprintln(os.Stderr, "curator decision journal: FAIL:", err)
	}
	if !ok {
		return 1
	}
	return 0
}
