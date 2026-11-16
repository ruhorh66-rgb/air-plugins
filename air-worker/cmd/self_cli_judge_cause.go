package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// cliProcessStarted bounds which judge verdict belongs to this process: only a
// .goal-verdict.json written after the CLI started is this run's verdict.
var cliProcessStarted = time.Now()

type judgeFailure struct {
	IDs          []string
	ChecksFailed int
	Ref          string
}

// judgeFailureDetail reads the verdict the judge of THIS process published
// (publishVerdict, judge.go) and keeps only fixed-shape facts: sanitised IDs of
// failed criteria (at most 16), the failed check count and a path#sha256 ref.
// No free text from checks or scripts is copied (A3.4, invariant 15: review
// on a failed check needs to know which check failed).
func judgeFailureDetail(argv []string) (judgeFailure, bool) {
	product := "."
	for i := 1; i < len(argv); i++ {
		switch a := argv[i]; {
		case a == "-product" || a == "--product":
			if i+1 < len(argv) {
				product = argv[i+1]
			}
		case strings.HasPrefix(a, "-product="), strings.HasPrefix(a, "--product="):
			product = a[strings.Index(a, "=")+1:]
		}
	}
	root, err := filepath.Abs(product)
	if err != nil {
		return judgeFailure{}, false
	}
	path := filepath.Join(root, ".goal-verdict.json")
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() {
		return judgeFailure{}, false
	}
	raw, err := readLearningBounded(path, 256*1024)
	if err != nil {
		return judgeFailure{}, false
	}
	var mv machineVerdict
	if json.Unmarshal(raw, &mv) != nil || mv.Code != 1 {
		return judgeFailure{}, false
	}
	at, err := time.Parse(time.RFC3339Nano, mv.At)
	if err != nil || at.Before(cliProcessStarted) {
		return judgeFailure{}, false
	}
	d := judgeFailure{ChecksFailed: mv.ChecksFailed, Ref: filepath.ToSlash(path) + "#sha256=" + learnSHA(raw)}
	for _, c := range mv.CriteriaFailed {
		if len(d.IDs) == 16 {
			break
		}
		if id := sanitizedCriterionID(c); id != "" {
			d.IDs = append(d.IDs, id)
		}
	}
	return d, true
}

// The first token of a criterion entry, restricted to letters, digits, '.',
// '_' and '-', at most 32 runes.
func sanitizedCriterionID(entry string) string {
	fields := strings.Fields(entry)
	if len(fields) == 0 {
		return ""
	}
	var b strings.Builder
	n := 0
	for _, r := range fields[0] {
		if n == 32 {
			break
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
			n++
		}
	}
	return b.String()
}

// ownCLIFailureObservationFor adds the judge verdict facts for a judge FAIL;
// every other cause keeps ownCLIFailureObservation.
func ownCLIFailureObservationFor(argv []string, action string, rc int, cause string) string {
	if cause != "judge-criteria-failed" {
		return ownCLIFailureObservation(action, rc, cause)
	}
	d, ok := judgeFailureDetail(argv)
	if !ok {
		return ownCLIFailureObservation(action, rc, "")
	}
	ids := "none listed"
	if len(d.IDs) > 0 {
		ids = strings.Join(d.IDs, ", ")
	}
	return fmt.Sprintf("Native AirWorker CLI action=%s ended with exit_code=%d. Trusted read-only judge verdict of this run: failed criteria %s; checks failed %d; verdict %s. No successful repair, procedure use or effect is claimed.",
		action, rc, ids, d.ChecksFailed, d.Ref)
}
