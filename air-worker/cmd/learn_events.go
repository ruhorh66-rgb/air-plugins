package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var learnEventKinds = map[string]bool{
	"lesson": true, "correction": true, "violation": true, "check": true, "judge_result": true,
}

func validateLearnEvent(row learnJournalRecord) error {
	if row.Schema != learnSchemaVersion {
		return fmt.Errorf("invalid event schema %q", row.Schema)
	}
	if strings.TrimSpace(row.ID) == "" {
		return errors.New("event id is required")
	}
	if _, err := time.Parse(time.RFC3339Nano, row.CreatedAt); err != nil {
		return fmt.Errorf("invalid event created_at: %w", err)
	}
	if !learnEventKinds[row.Kind] {
		return fmt.Errorf("invalid event kind %q", row.Kind)
	}
	if _, err := cleanLearnText("source", row.Source); err != nil {
		return err
	}
	if _, err := cleanLearnText("class", row.Class); err != nil {
		return err
	}
	if _, err := cleanLearnText("observed", row.Observed); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"evidence": row.Evidence, "actor": row.Actor, "reference": row.Reference,
		"source_timestamp": row.SourceTimestamp,
	} {
		limit := 2000
		if name == "source_timestamp" {
			limit = 200
		}
		if len([]rune(value)) > limit {
			return fmt.Errorf("-%s is too long", name)
		}
	}
	return nil
}

type legacyCuratorEvent struct {
	TS              string   `json:"ts"`
	Situation       string   `json:"situation"`
	Curator         string   `json:"curator"`
	LPR             string   `json:"lpr"`
	Class           string   `json:"class"`
	Lesson          string   `json:"lesson"`
	What            string   `json:"what"`
	Rule            string   `json:"rule"`
	Mechanism       string   `json:"mechanism"`
	MechanismNeeded string   `json:"mechanism_needed"`
	Repeat          int      `json:"repeat"`
	Links           []string `json:"links"`
}

func stableImportedEvent(shape string, lineNumber int, line []byte, created, sourceTimestamp, class, observed, evidence, actor, reference, source string) learnJournalRecord {
	identity := append([]byte(shape+"\n"+strconv.Itoa(lineNumber)+"\n"), line...)
	fingerprint := learnSHA(identity)
	return learnJournalRecord{
		Schema: learnSchemaVersion, ID: "LE-" + fingerprint[:20], CreatedAt: created,
		Kind: "lesson", Source: source, Actor: actor, Reference: reference,
		Class: class, Observed: observed, Evidence: evidence, ImportID: fingerprint,
		SourceTimestamp: sourceTimestamp,
	}
}

func legacyCuratorObservedEvidence(old legacyCuratorEvent) (string, string) {
	observed := strings.TrimSpace(old.Lesson)
	if observed == "" {
		observed = strings.TrimSpace(old.Rule)
	}
	if observed == "" {
		observed = strings.TrimSpace(old.What)
	}
	if observed == "" {
		observed = strings.TrimSpace(old.Situation)
	}

	evidence := strings.TrimSpace(old.Situation)
	if evidence == "" {
		evidence = strings.TrimSpace(old.What)
	}
	var extras []string
	for _, item := range []struct {
		name  string
		value string
	}{
		{name: "rule", value: old.Rule},
		{name: "what", value: old.What},
	} {
		value := strings.TrimSpace(item.value)
		if value != "" && value != observed && value != evidence {
			extras = append(extras, item.name+"="+value)
		}
	}
	if strings.TrimSpace(old.Mechanism) != "" {
		extras = append(extras, "mechanism="+strings.TrimSpace(old.Mechanism))
	}
	if strings.TrimSpace(old.MechanismNeeded) != "" {
		extras = append(extras, "mechanism_needed="+strings.TrimSpace(old.MechanismNeeded))
	}
	if old.Repeat != 0 {
		extras = append(extras, fmt.Sprintf("repeat=%d", old.Repeat))
	}
	if len(old.Links) > 0 {
		extras = append(extras, "links="+strings.Join(old.Links, ","))
	}
	if len(extras) > 0 {
		if evidence != "" {
			evidence += " | "
		}
		evidence += strings.Join(extras, " | ")
	}
	return observed, evidence
}

func importLegacyEvents(target, sourcePath, shape string) (int, error) {
	info, err := os.Stat(sourcePath)
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("source is not a regular file: %s", sourcePath)
	}
	var candidates []learnJournalRecord
	err = scanLearnJSONLIndexed(sourcePath, func(lineNumber int, line []byte) error {
		switch shape {
		case "product":
			var old learnJournalRecord
			if err := json.Unmarshal(line, &old); err != nil {
				return err
			}
			created, err := normalizeLegacyTimestamp(old.CreatedAt)
			if err != nil {
				return err
			}
			candidates = append(candidates, stableImportedEvent(shape, lineNumber, line, created, old.CreatedAt, old.Class, old.Observed, old.Evidence, "", old.ID, "air-worker-0.10"))
		case "aircurator":
			var old legacyCuratorEvent
			if err := json.Unmarshal(line, &old); err != nil {
				return err
			}
			created, err := normalizeLegacyTimestamp(old.TS)
			if err != nil {
				return err
			}
			observed, evidence := legacyCuratorObservedEvidence(old)
			candidates = append(candidates, stableImportedEvent(shape, lineNumber, line, created, old.TS, old.Class, observed, evidence, old.Curator, old.LPR, "aircurator"))
		default:
			return fmt.Errorf("unknown legacy event shape %q", shape)
		}
		return validateLearnEvent(candidates[len(candidates)-1])
	})
	if err != nil {
		return 0, err
	}
	existing, err := readLearnEventsNoMigration(target)
	if err != nil {
		return 0, err
	}
	seen := make(map[string]bool, len(existing))
	for _, row := range existing {
		seen[row.ImportID] = true
		seen[row.ID] = true
	}
	added := 0
	for _, row := range candidates {
		if seen[row.ImportID] || seen[row.ID] {
			continue
		}
		if err := appendLearnJSON(target, row); err != nil {
			return added, err
		}
		seen[row.ImportID], seen[row.ID] = true, true
		added++
	}
	return added, nil
}

// Legacy journals used several timestamp shapes over the same two days. Some rows
// carry an explicit offset, while others are floating wall-clock values written under
// different machine timezones. A single inferred timezone would therefore fabricate
// history. The original value is preserved separately in source_timestamp; created_at
// is only a deterministic machine-ordering projection:
//   - explicit offsets retain their instant and normalize to UTC;
//   - date-only values become midnight UTC;
//   - floating local values keep their wall-clock fields and are projected onto UTC.
func normalizeLegacyTimestamp(value string) (string, error) {
	value = strings.TrimSpace(value)
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t.UTC().Format(time.RFC3339Nano), nil
	}
	if t, err := time.Parse("2006-01-02T15:04Z07:00", value); err == nil {
		return t.UTC().Format(time.RFC3339Nano), nil
	}
	for _, layout := range []string{
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04",
		"2006-01-02",
	} {
		if t, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
			return t.UTC().Format(time.RFC3339Nano), nil
		}
	}
	return "", fmt.Errorf("invalid legacy timestamp %q", value)
}

func readLearnEventsNoMigration(path string) ([]learnJournalRecord, error) {
	var out []learnJournalRecord
	err := scanLearnJSONL(path, func(b []byte) error {
		var row learnJournalRecord
		if err := json.Unmarshal(b, &row); err != nil {
			return err
		}
		if err := validateLearnEvent(row); err != nil {
			return err
		}
		out = append(out, row)
		return nil
	})
	return out, err
}

func migrateProductJournal(product string) error {
	paths := learnPaths(product)
	legacy := filepath.Join(paths.Root, "journal.jsonl")
	if _, err := os.Stat(legacy); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	_, err := importLegacyEvents(paths.Journal, legacy, "product")
	return err
}

func cmdLearnMigrateLegacy(argv []string) int {
	fs := flag.NewFlagSet("learn migrate-legacy", flag.ContinueOnError)
	product := fs.String("product", "", "managed product root")
	source := fs.String("source", "", "read-only AirCurator journal.jsonl path")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	root, err := normalizeLearnProduct(*product)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	absSource, err := filepath.Abs(strings.TrimSpace(*source))
	if err != nil || strings.TrimSpace(*source) == "" {
		fmt.Fprintln(os.Stderr, "-source is required")
		return 2
	}
	added, err := importLegacyEvents(learnPaths(root).Journal, absSource, "aircurator")
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn migrate-legacy:", err)
		return 2
	}
	fmt.Printf("IMPORTED %d\n", added)
	return 0
}
