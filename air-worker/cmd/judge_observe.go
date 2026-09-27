package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const judgeObservationSchema = "air-worker.judge/v1"

type judgeCheckObservation struct {
	Name       string   `json:"name"`
	State      string   `json:"state"`
	Reason     string   `json:"reason,omitempty"`
	DurationMS int64    `json:"duration_ms"`
	Script     string   `json:"script,omitempty"`
	Command    string   `json:"command,omitempty"`
	Args       []string `json:"args,omitempty"`
	Select     string   `json:"select,omitempty"`
}

type judgeMeasureObservation struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Selector   string `json:"selector,omitempty"`
	State      string `json:"state"`
	Reason     string `json:"reason,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

type judgeCriterionObservation struct {
	ID         string                    `json:"id"`
	Goal       string                    `json:"goal,omitempty"`
	Sign       string                    `json:"sign,omitempty"`
	Measure    string                    `json:"measure,omitempty"`
	State      string                    `json:"state"`
	Reason     string                    `json:"reason,omitempty"`
	DurationMS int64                     `json:"duration_ms"`
	Measures   []judgeMeasureObservation `json:"measures,omitempty"`
}

type judgeJSONReport struct {
	Schema           string                      `json:"schema"`
	Product          string                      `json:"product"`
	At               string                      `json:"at"`
	Code             int                         `json:"code"`
	VerdictText      string                      `json:"verdict_text"`
	Distance         *int                        `json:"distance"`
	InputFingerprint string                      `json:"input_fingerprint"`
	Checks           []judgeCheckObservation     `json:"checks"`
	Criteria         []judgeCriterionObservation `json:"criteria"`
	LPRGates         int                         `json:"lpr_gates"`
	FactsClosed      *int                        `json:"facts_closed,omitempty"`
	FactsGated       int                         `json:"facts_gated"`
	FactsRequired    int                         `json:"facts_required"`
	FactsOverlap     []string                    `json:"facts_overlap,omitempty"`
}

func measureStateText(state measureState) string {
	switch state {
	case measurePass:
		return "PASS"
	case measureFail:
		return "FAIL"
	case measureGated:
		return "GATED"
	default:
		return "UNKNOWN"
	}
}

func judgeCheckState(r judgeResult, name string) (state, reason string) {
	mr := checkResultState(r, name)
	return measureStateText(mr.State), mr.Detail
}

func judgeInputPaths(root string, cfg runConfig, configPath, planPath string) []string {
	var raw []string
	raw = append(raw, configPath, planPath, cfg.Judge.Path, cfg.Judge.Checklist)
	for _, check := range cfg.Judge.Checks {
		raw = append(raw, check.Script)
	}
	seen := map[string]bool{}
	var out []string
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !filepath.IsAbs(item) {
			item = filepath.Join(root, item)
		}
		abs, err := filepath.Abs(item)
		if err != nil {
			abs = filepath.Clean(item)
		}
		key := strings.ToLower(filepath.Clean(abs))
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, filepath.Clean(abs))
	}
	sort.Strings(out)
	return out
}

func judgeInputFingerprint(root string, cfg runConfig, configPath, planPath string) string {
	h := sha256.New()
	for _, path := range judgeInputPaths(root, cfg, configPath, planPath) {
		rel, err := filepath.Rel(root, path)
		if err != nil || strings.HasPrefix(rel, "..") {
			rel = path
		}
		_, _ = h.Write([]byte(filepath.ToSlash(rel)))
		_, _ = h.Write([]byte{0})
		raw, err := os.ReadFile(path)
		if err != nil {
			_, _ = h.Write([]byte("MISSING:" + err.Error()))
		} else {
			sum := sha256.Sum256(raw)
			_, _ = h.Write([]byte(hex.EncodeToString(sum[:])))
		}
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func machineVerdictFromResult(root string, code int, text string, r judgeResult) machineVerdict {
	passed := len(r.Passed)
	failed := len(r.Failed)
	if r.FactsLine != "" {
		for _, p := range r.Passed {
			if p == r.FactsLine {
				passed--
				break
			}
		}
		for _, f := range r.Failed {
			if f == r.FactsLine {
				failed--
				break
			}
		}
	}
	return machineVerdict{
		At:               nowLocalMachineVerdict(),
		Code:             code,
		Distance:         distanceOf(code, r),
		InputFingerprint: r.InputFingerprint,
		ChecksPassed:     passed,
		ChecksFailed:     failed,
		ChecksUnknown:    len(r.Unknown),
		CriteriaPassed:   r.CriteriaPassed,
		CriteriaFailed:   r.CriteriaFailed,
		CriteriaGated:    r.CriteriaGated,
		CriteriaUnknown:  r.CriteriaUnknown,
		CriteriaTotal:    len(r.CriteriaPassed) + len(r.CriteriaFailed) + len(r.CriteriaGated) + len(r.CriteriaUnknown),
		LPRGates:         r.PlanGates + len(r.CriteriaGated),
		FactsClosed:      r.FactsClosed,
		FactsGated:       r.FactsGated,
		FactsRequired:    r.FactsRequired,
		FactsOverlap:     r.FactsOverlap,
		VerdictText:      text,
		By:               appName + " " + version,
	}
}

func nowLocalMachineVerdict() string {
	return time.Now().Format("2006-01-02T15:04:05")
}

func buildJudgeJSONReport(root string, mv machineVerdict, r judgeResult) judgeJSONReport {
	return judgeJSONReport{
		Schema: judgeObservationSchema, Product: root, At: mv.At,
		Code: mv.Code, VerdictText: mv.VerdictText, Distance: mv.Distance,
		InputFingerprint: mv.InputFingerprint,
		Checks:           append([]judgeCheckObservation(nil), r.CheckObservations...),
		Criteria:         append([]judgeCriterionObservation(nil), r.CriterionObservations...),
		LPRGates:         mv.LPRGates, FactsClosed: mv.FactsClosed, FactsGated: mv.FactsGated,
		FactsRequired: mv.FactsRequired, FactsOverlap: append([]string(nil), mv.FactsOverlap...),
	}
}

func marshalJudgeJSON(report judgeJSONReport) []byte {
	raw, _ := json.MarshalIndent(report, "", "  ")
	return raw
}
