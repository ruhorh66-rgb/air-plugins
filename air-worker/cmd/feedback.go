package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const feedbackMarker = "<!-- air-worker:feedback-candidates -->"

type feedbackRecord struct {
	FeedbackID      string `json:"feedback_id"`
	CreatedAt       string `json:"created_at"`
	Status          string `json:"status"`
	Product         string `json:"product"`
	SourceVersion   string `json:"source_version"`
	Type            string `json:"type"`
	Severity        string `json:"severity"`
	Observed        string `json:"observed"`
	Expected        string `json:"expected"`
	Evidence        string `json:"evidence"`
	Reproduction    string `json:"reproduction"`
	Workaround      string `json:"workaround"`
	ProposedOutcome string `json:"proposed_outcome"`
}

type feedbackWriteResult struct {
	FeedbackID      string
	EvidencePath    string
	PlanPath        string
	EvidenceWritten bool
	PlanWritten     bool
}

type feedbackIO struct {
	writeEvidence func(string, []byte) error
	writePlan     func(string, []byte) error
}

func defaultFeedbackIO() feedbackIO {
	return feedbackIO{
		writeEvidence: writeImmutableFeedbackFile,
		writePlan:     writeFileAtomic,
	}
}

func newFeedbackID(now time.Time) (string, error) {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", err
	}
	return "FB-" + now.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(suffix[:]), nil
}

func feedbackEvidenceRel(id string) string {
	return filepath.ToSlash(filepath.Join(".air-worker", "feedback", id+".json"))
}

func validateFeedback(r feedbackRecord) error {
	fields := []struct {
		name, value string
	}{
		{"product", r.Product}, {"source_version", r.SourceVersion}, {"type", r.Type},
		{"severity", r.Severity}, {"observed", r.Observed}, {"expected", r.Expected},
		{"evidence", r.Evidence}, {"reproduction", r.Reproduction},
		{"workaround", r.Workaround}, {"proposed_outcome", r.ProposedOutcome},
	}
	for _, f := range fields {
		if strings.TrimSpace(f.value) == "" {
			return fmt.Errorf("required field is empty: %s", f.name)
		}
	}
	switch r.Type {
	case "defect", "friction", "idea":
	default:
		return fmt.Errorf("type must be defect, friction, or idea: %q", r.Type)
	}
	switch r.Severity {
	case "P0", "P1", "P2", "P3":
	default:
		return fmt.Errorf("severity must be P0, P1, P2, or P3: %q", r.Severity)
	}
	return nil
}
func feedbackPlanText(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.ReplaceAll(s, "|", "/")
	s = strings.ReplaceAll(s, "`", "'")
	const max = 220
	if len([]rune(s)) > max {
		r := []rune(s)
		s = string(r[:max]) + "..."
	}
	return s
}

func feedbackCandidateLine(r feedbackRecord, rel string) string {
	return fmt.Sprintf("- feedback `%s` · status=`candidate` · type=`%s` · severity=`%s` · source=`%s` · observed=%s · evidence=`%s`",
		r.FeedbackID, feedbackPlanText(r.Type), feedbackPlanText(r.Severity),
		feedbackPlanText(r.SourceVersion), feedbackPlanText(r.Observed), rel)
}

func planWithFeedbackCandidate(raw []byte, r feedbackRecord, rel string) []byte {
	s := string(raw)
	nl := "\n"
	if strings.Contains(s, "\r\n") {
		nl = "\r\n"
	}
	line := feedbackCandidateLine(r, rel)
	if strings.Contains(s, r.FeedbackID) {
		return raw
	}
	if i := strings.Index(s, feedbackMarker); i >= 0 {
		insertAt := i + len(feedbackMarker)
		return []byte(s[:insertAt] + nl + line + s[insertAt:])
	}
	if !strings.HasSuffix(s, "\n") {
		s += nl
	}
	return []byte(s + nl + "## Operational feedback candidates" + nl + nl + feedbackMarker + nl + line + nl)
}
func writeImmutableFeedbackFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("feedback evidence already exists: %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeFileAtomic(path, data)
}

func writeFeedbackWithIO(root, planPath string, r feedbackRecord, io feedbackIO) (feedbackWriteResult, error) {
	rel := feedbackEvidenceRel(r.FeedbackID)
	res := feedbackWriteResult{
		FeedbackID:   r.FeedbackID,
		EvidencePath: filepath.Join(root, filepath.FromSlash(rel)),
		PlanPath:     planPath,
	}
	body, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return res, err
	}
	body = append(body, '\n')
	if err := io.writeEvidence(res.EvidencePath, body); err != nil {
		return res, fmt.Errorf("evidence write failed: %w", err)
	}
	res.EvidenceWritten = true

	planRaw, err := os.ReadFile(planPath)
	if err != nil {
		return res, fmt.Errorf("plan write missing: cannot read %s: %w", planPath, err)
	}
	updated := planWithFeedbackCandidate(planRaw, r, rel)
	if err := io.writePlan(planPath, updated); err != nil {
		return res, fmt.Errorf("plan write failed: %w", err)
	}
	res.PlanWritten = true
	return res, nil
}
func cmdFeedback(argv []string) int {
	fs := flag.NewFlagSet("feedback", flag.ContinueOnError)
	product := fs.String("product", "", "product root")
	sourceVersion := fs.String("source-version", "", "observed product version")
	kind := fs.String("type", "", "defect|friction|idea")
	severity := fs.String("severity", "", "P0|P1|P2|P3")
	observed := fs.String("observed", "", "what happened")
	expected := fs.String("expected", "", "what should happen")
	evidence := fs.String("evidence", "", "receipt/log/path/reference")
	reproduction := fs.String("reproduction", "", "how to reproduce")
	workaround := fs.String("workaround", "", "current workaround or none")
	proposedOutcome := fs.String("proposed-outcome", "", "desired product outcome")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if strings.TrimSpace(*product) == "" {
		fmt.Fprint(os.Stderr, "feedback: -product is required"+lineEnding)
		return 2
	}
	root, err := filepath.Abs(*product)
	if err != nil {
		fmt.Fprintf(os.Stderr, "feedback: product path: %v%s", err, lineEnding)
		return 2
	}
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		fmt.Fprintf(os.Stderr, "feedback: product root is not a directory: %s%s", root, lineEnding)
		return 2
	}
	id, err := newFeedbackID(time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "feedback: cannot generate id: %v%s", err, lineEnding)
		return 1
	}
	r := feedbackRecord{
		FeedbackID:      id,
		CreatedAt:       time.Now().UTC().Format(time.RFC3339Nano),
		Status:          "candidate",
		Product:         filepath.Base(filepath.Clean(root)),
		SourceVersion:   strings.TrimSpace(*sourceVersion),
		Type:            strings.ToLower(strings.TrimSpace(*kind)),
		Severity:        strings.ToUpper(strings.TrimSpace(*severity)),
		Observed:        strings.TrimSpace(*observed),
		Expected:        strings.TrimSpace(*expected),
		Evidence:        strings.TrimSpace(*evidence),
		Reproduction:    strings.TrimSpace(*reproduction),
		Workaround:      strings.TrimSpace(*workaround),
		ProposedOutcome: strings.TrimSpace(*proposedOutcome),
	}
	if err := validateFeedback(r); err != nil {
		fmt.Fprint(os.Stderr, "feedback: "+err.Error()+lineEnding)
		return 2
	}

	var cfg runConfig
	_ = readJSON(filepath.Join(root, "run-config.json"), &cfg)
	planPath := planFilePath(root, cfg)
	lock, ok := acquireLock(lockName("feedback", root))
	if !ok {
		fmt.Fprint(os.Stderr, "feedback: another feedback write is active for this product"+lineEnding)
		return 1
	}
	defer lock.release()

	res, err := writeFeedbackWithIO(root, planPath, r, defaultFeedbackIO())
	fmt.Printf("feedback_id : %s%s", r.FeedbackID, lineEnding)
	if res.EvidenceWritten {
		fmt.Printf("evidence    : written %s%s", res.EvidencePath, lineEnding)
	} else {
		fmt.Printf("evidence    : MISSING %s%s", res.EvidencePath, lineEnding)
	}
	if res.PlanWritten {
		fmt.Printf("plan        : candidate %s%s", res.PlanPath, lineEnding)
	} else {
		fmt.Printf("plan        : MISSING %s%s", res.PlanPath, lineEnding)
	}
	if err != nil {
		if res.EvidenceWritten {
			fmt.Printf("status      : PARTIAL — %v%s", err, lineEnding)
		} else {
			fmt.Printf("status      : FAILED — %v%s", err, lineEnding)
		}
		return 1
	}
	fmt.Print("status      : OK" + lineEnding)
	return 0
}
