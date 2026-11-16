package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
)

// The CLI failure is captured in the same process that ran `air-worker judge`,
// so the verdict this process published is exactly this run's verdict. It is
// kept in memory instead of being read back from .goal-verdict.json: no other
// process's verdict and no file swapped in between can be taken for it.
var publishedVerdict struct {
	sync.Mutex
	root string
	mv   machineVerdict
	sha  string
}

func notePublishedVerdict(root string, mv machineVerdict, raw []byte) {
	publishedVerdict.Lock()
	defer publishedVerdict.Unlock()
	publishedVerdict.root, publishedVerdict.mv, publishedVerdict.sha = root, mv, learnSHA(raw)
}

type judgeFailure struct {
	IDs          []string
	ChecksFailed int
	Ref          string
}

// judgeFailureDetail keeps only fixed-shape facts of this process's FAIL
// verdict for the product named in argv: sanitised IDs of failed criteria (at
// most 16), the failed check count and the hash of the published verdict file.
// No free text and no path is copied (A3.4, invariant 15: review on a failed
// check needs to know which check failed).
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
	publishedVerdict.Lock()
	vRoot, mv, sha := publishedVerdict.root, publishedVerdict.mv, publishedVerdict.sha
	publishedVerdict.Unlock()
	if sha == "" || !sameLearningPath(vRoot, root) || mv.Code != 1 {
		return judgeFailure{}, false
	}
	d := judgeFailure{ChecksFailed: mv.ChecksFailed, Ref: ".goal-verdict.json#sha256=" + sha}
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
	return fmt.Sprintf("Native AirWorker CLI action=%s ended with exit_code=%d. Trusted judge verdict of this run: failed criteria %s; checks failed %d; verdict %s. No successful repair, procedure use or effect is claimed.",
		action, rc, ids, d.ChecksFailed, d.Ref)
}
