package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const releaseSlicesSchema = "air-worker.release-slices/v1"

type releaseSlicesSummary struct {
	Found         bool                   `json:"found"`
	ActiveVersion string                 `json:"active_version,omitempty"`
	NextStep      string                 `json:"next_step,omitempty"`
	ReleasesCount int                    `json:"releases_count"`
	StepsCount    int                    `json:"steps_count"`
	ActiveClosed  int                    `json:"active_closed"`
	ActiveTotal   int                    `json:"active_total"`
	ActiveStep    releaseStep            `json:"-"`
	HasActiveStep bool                   `json:"-"`
	Problems      []releaseSlicesProblem `json:"problems"`
}

type releaseSlicesProblem struct {
	Rule   string `json:"rule"`
	Detail string `json:"detail"`
}

type releaseSlicesDoc struct {
	Schema            string               `json:"schema"`
	Policy            string               `json:"policy"`
	Product           string               `json:"product"`
	DecisionSGT       string               `json:"decision_sgt"`
	Trigger           string               `json:"trigger"`
	ActiveRelease     string               `json:"active_release"`
	InstalledBase     string               `json:"installed_baseline"`
	AuthorityBranch   string               `json:"authority_branch"`
	MachinePlan       string               `json:"machine_plan"`
	NativeNodes       string               `json:"native_nodes"`
	ReleaseGatePolicy string               `json:"release_gate_policy"`
	IntervalPolicy    map[string]any       `json:"interval_policy"`
	Releases          []releaseSlice       `json:"releases"`
	VersionPolicy     releaseVersionPolicy `json:"version_policy"`
	MilestoneSequence json.RawMessage      `json:"milestone_sequence"`
}

type releaseVersionPolicy struct {
	Format             string `json:"format"`
	Basis              string `json:"basis"`
	NextVersion        string `json:"next_version"`
	Reason             string `json:"reason"`
	Reserved           string `json:"preserve_prior_architecture_reservation"`
	TagPrefix          string `json:"remote_tag_prefix"`
	OrdinalRule        string `json:"version_is_not_ordinal"`
	Guard              string `json:"guard"`
	PlannedPatchTail   string `json:"planned_patch_tail"`
	VerifiedPrecedence string `json:"verified_precedence"`
}

func (p *releaseVersionPolicy) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for key, target := range map[string]*string{
		"format": &p.Format, "basis": &p.Basis, "next_version": &p.NextVersion,
		"reason": &p.Reason, "preserve_prior_architecture_reservation": &p.Reserved,
		"remote_tag_prefix": &p.TagPrefix, "version_is_not_ordinal": &p.OrdinalRule, "guard": &p.Guard,
		"planned_patch_tail": &p.PlannedPatchTail, "verified_precedence": &p.VerifiedPrecedence,
	} {
		value, ok := fields[key]
		if !ok {
			return fmt.Errorf("missing version_policy field %q", key)
		}
		if err := json.Unmarshal(value, target); err != nil {
			return err
		}
		delete(fields, key)
	}
	if len(fields) != 0 {
		for key := range fields {
			return fmt.Errorf("unknown version_policy field %q", key)
		}
	}
	return nil
}

type releaseSlice struct {
	Version         string             `json:"version"`
	Status          string             `json:"status"`
	Stage           string             `json:"stage"`
	Feature         string             `json:"feature"`
	Owner           string             `json:"owner"`
	Trigger         string             `json:"trigger"`
	DoneWhen        string             `json:"done_when"`
	Judge           string             `json:"judge"`
	Receipt         string             `json:"receipt"`
	RollbackTo      string             `json:"rollback_to"`
	Dependencies    []string           `json:"dependencies"`
	PreReleaseGates []releaseSliceGate `json:"pre_release_gates"`
	NodeRefs        []string           `json:"node_refs"`
	Deferred        []string           `json:"deferred"`
	Steps           []releaseStep      `json:"steps"`
}

type releaseSliceGate struct {
	ID             string `json:"id"`
	Status         string `json:"status"`
	RequiredBefore string `json:"required_before"`
	Requirement    string `json:"requirement"`
}

type releaseStep struct {
	ID            string   `json:"id"`
	Status        string   `json:"status"`
	Owner         string   `json:"owner"`
	Trigger       string   `json:"trigger"`
	DoneWhen      string   `json:"done_when"`
	Judge         string   `json:"judge"`
	Receipt       string   `json:"receipt"`
	RollbackTo    string   `json:"rollback_to"`
	BudgetMinutes int      `json:"budget_minutes"`
	Title         string   `json:"title"`
	Kind          string   `json:"kind"`
	NodeRefs      []string `json:"node_refs"`
}

var releaseSemver = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func inspectReleaseSlices(productRoot string) releaseSlicesSummary {
	summary := releaseSlicesSummary{Problems: []releaseSlicesProblem{}}
	root, err := filepath.Abs(productRoot)
	if err != nil {
		summary.problem("read", err.Error())
		return summary
	}
	path := filepath.Join(root, "plan", "release-slices.json")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return summary
	}
	if err != nil {
		summary.Found = true
		summary.problem("read", err.Error())
		return summary
	}
	summary.Found = true
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		summary.problem("read", err.Error())
		return summary
	}
	if len(raw) > 1<<20 {
		summary.problem("size", "file exceeds 1 MiB")
		return summary
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		summary.problem("json", err.Error())
		return summary
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var doc releaseSlicesDoc
	if err := dec.Decode(&doc); err != nil {
		summary.problem("json", err.Error())
		return summary
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		summary.problem("json", "trailing JSON data")
		return summary
	}
	validateReleaseSlices(root, doc, &summary)
	return summary
}

func (s *releaseSlicesSummary) problem(rule, detail string) {
	s.Problems = append(s.Problems, releaseSlicesProblem{Rule: rule, Detail: detail})
}

func validateReleaseSlices(root string, doc releaseSlicesDoc, out *releaseSlicesSummary) {
	if doc.Schema != releaseSlicesSchema {
		out.problem("schema", "schema must be "+releaseSlicesSchema)
	}
	if doc.InstalledBase != "0.11.8" || !strings.Contains(doc.VersionPolicy.Reserved, "0.12.0") {
		out.problem("version-policy", "installed baseline must be 0.11.8 and reserved architecture version must be 0.12.0")
	}
	expectedReleases, rangeOK := releaseCountFromVersionPolicy(doc.VersionPolicy.NextVersion, doc.VersionPolicy.PlannedPatchTail)
	if !rangeOK {
		out.problem("version-policy", "next_version and planned_patch_tail must define one numeric patch train")
	} else if len(doc.Releases) != expectedReleases {
		out.problem("release-count", fmt.Sprintf("expected %d releases from version policy, got %d", expectedReleases, len(doc.Releases)))
	}
	out.ReleasesCount = len(doc.Releases)
	versions := map[string]int{}
	stepIDs, receipts := map[string]bool{}, map[string]bool{}
	ordinals := map[int]bool{}
	activeCount, openCount := 0, 0
	activeVersion := doc.ActiveRelease
	var activeOpen string
	for i, r := range doc.Releases {
		major, minor, patch, ok := parseReleaseVersion(r.Version)
		if !ok {
			out.problem("version", fmt.Sprintf("release %q is not strict numeric SemVer X.Y.Z", r.Version))
		} else if i > 0 {
			pm, pn, pp, prevOK := parseReleaseVersion(doc.Releases[i-1].Version)
			if prevOK && compareVersion(major, minor, patch, pm, pn, pp) <= 0 {
				out.problem("version-order", "release versions must be strictly numerically ascending")
			}
		}
		if _, exists := versions[r.Version]; exists {
			out.problem("version-duplicate", "duplicate release version "+r.Version)
		}
		versions[r.Version] = i
		if r.Status == "active" {
			activeCount++
			activeVersion = r.Version
		} else if r.Status != "planned" {
			out.problem("release-status", "release status must be active or planned")
		}
		if len(r.Steps) < 1 || len(r.Steps) > 7 {
			out.problem("step-count", "each release must contain 1..7 steps")
		}
		if r.RollbackTo == "" {
			out.problem("rollback", "release rollback_to is required")
		}
		for _, dep := range r.Dependencies {
			idx, exists := versions[dep]
			if !exists || idx >= i {
				out.problem("dependency", "dependencies must reference earlier releases: "+dep)
			}
		}
		for _, st := range r.Steps {
			out.StepsCount++
			if r.Status == "active" {
				out.ActiveTotal++
				if st.Status == "closed" {
					out.ActiveClosed++
				}
			}
			prefix := "R" + r.Version + "-"
			if !strings.HasPrefix(st.ID, prefix) {
				out.problem("step-prefix", "step ID has wrong version prefix: "+st.ID)
			}
			if stepIDs[st.ID] {
				out.problem("step-duplicate", "duplicate step ID "+st.ID)
			}
			stepIDs[st.ID] = true
			ordText := strings.TrimPrefix(st.ID, prefix)
			ord, err := strconv.Atoi(ordText)
			ordinalKey := i*100 + ord
			if err != nil || ord < 1 || ordinals[ordinalKey] {
				out.problem("step-ordinal", "step ordinal must be unique positive numeric within release: "+st.ID)
			} else {
				ordinals[ordinalKey] = true
			}
			if st.BudgetMinutes < 1 || st.BudgetMinutes > 15 {
				out.problem("budget", "step budget_minutes must be 1..15: "+st.ID)
			}
			for name, value := range map[string]string{"owner": st.Owner, "trigger": st.Trigger, "done_when": st.DoneWhen, "judge": st.Judge, "receipt": st.Receipt} {
				if strings.TrimSpace(value) == "" {
					out.problem("required", name+" is required for "+st.ID)
				}
			}
			if st.Receipt != "" {
				if receipts[st.Receipt] {
					out.problem("receipt-duplicate", "duplicate receipt "+st.Receipt)
				}
				receipts[st.Receipt] = true
				if err := safeReleaseReceipt(root, st.Receipt); err != nil {
					out.problem("receipt-path", st.Receipt+": "+err.Error())
				}
			}
			switch st.Status {
			case "open":
				openCount++
				if r.Status == "active" {
					activeOpen = st.ID
					out.ActiveStep = st
					out.HasActiveStep = true
				}
			case "planned", "running", "closed", "failed":
			default:
				out.problem("step-status", "invalid step status for "+st.ID)
			}
		}
	}
	if activeCount != 1 {
		out.problem("active-release", "exactly one release must be active")
	}
	if activeVersion != doc.ActiveRelease {
		out.problem("active-version", "active_release must match the active release")
	}
	if openCount != 1 || activeOpen == "" {
		out.problem("open-step", "exactly one open step must exist and belong to the active release")
	}
	if activeOpen != "" {
		out.NextStep = activeOpen
	}
	if doc.VersionPolicy.NextVersion != "0.11.9" || doc.ActiveRelease != "0.11.9" {
		out.problem("version-policy", "first release and next_version must be 0.11.9")
	}
	out.ActiveVersion = activeVersion
}

func releaseCountFromVersionPolicy(start, tail string) (int, bool) {
	startMajor, startMinor, startPatch, startOK := parseReleaseVersion(start)
	tailMajor, tailMinor, tailPatch, tailOK := parseReleaseVersion(tail)
	if !startOK || !tailOK || startMajor != tailMajor || startMinor != tailMinor || tailPatch < startPatch {
		return 0, false
	}
	return tailPatch - startPatch + 1, true
}

func parseReleaseVersion(s string) (int, int, int, bool) {
	parts := releaseSemver.FindStringSubmatch(s)
	if parts == nil {
		return 0, 0, 0, false
	}
	a, ea := strconv.Atoi(parts[1])
	b, eb := strconv.Atoi(parts[2])
	c, ec := strconv.Atoi(parts[3])
	return a, b, c, ea == nil && eb == nil && ec == nil
}

func compareVersion(a, b, c, x, y, z int) int {
	if a != x {
		if a < x {
			return -1
		}
		return 1
	}
	if b != y {
		if b < y {
			return -1
		}
		return 1
	}
	if c < z {
		return -1
	}
	if c > z {
		return 1
	}
	return 0
}

func safeReleaseReceipt(root, name string) error {
	if strings.Contains(name, `\`) || filepath.IsAbs(name) || strings.Contains(name, ":") {
		return fmt.Errorf("must be a relative slash path")
	}
	parts := strings.Split(name, "/")
	if len(parts) < 3 || parts[0] != "docs" || parts[1] != "receipts" || !strings.HasSuffix(name, ".json") {
		return fmt.Errorf("must be under docs/receipts/*.json")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("unsafe path component")
		}
	}
	cur := root
	for _, part := range parts[:len(parts)-1] {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink ancestor")
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func rejectDuplicateJSONKeys(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var walk func() error
	walk = func() error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return err
				}
				key := keyTok.(string)
				if seen[key] {
					return fmt.Errorf("duplicate object key %q", key)
				}
				seen[key] = true
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		case '[':
			for dec.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		}
		return fmt.Errorf("invalid JSON delimiter")
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	return nil
}
