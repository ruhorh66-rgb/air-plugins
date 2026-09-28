package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	planNodeDirName = "plan"
	planNodeMarker  = "<!-- air-worker-plan-nodes -->"
)

var (
	planNodeIDRE = regexp.MustCompile(`^N-([0-9]{3})_([\p{L}\p{N}][\p{L}\p{N}_-]*)$`)
	planNodeNow  = time.Now
)

type planNode struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Parent    string   `json:"parent"`
	Trigger   string   `json:"trigger"`
	Owner     string   `json:"owner"`
	DoneWhen  string   `json:"done_when"`
	Status    string   `json:"status"`
	ReturnTo  string   `json:"return_to"`
	Receipts  []string `json:"receipts"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
	Path      string   `json:"path,omitempty"`
}

func normalizePlanNodeProduct(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		raw = "."
	}
	root, err := filepath.Abs(raw)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(root)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return "", fmt.Errorf("product is not a directory: %s", root)
	}
	if st, err := os.Stat(filepath.Join(root, "PLAN.md")); err != nil || st.IsDir() {
		return "", fmt.Errorf("PLAN.md is required in %s", root)
	}
	return filepath.Clean(root), nil
}

func planNodeDir(root string) string { return filepath.Join(root, planNodeDirName) }

func planNodeSlug(title string) string {
	title = strings.TrimSpace(strings.ToLower(title))
	var b strings.Builder
	dash := false
	for _, r := range title {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		case r == '-' || r == '_' || unicode.IsSpace(r):
			dash = true
		default:
			dash = true
		}
		if b.Len() >= 48 {
			break
		}
	}
	s := strings.Trim(b.String(), "-_")
	if s == "" {
		return "node"
	}
	return s
}

func nextPlanNodeID(root, title string) (string, error) {
	entries, err := os.ReadDir(planNodeDir(root))
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	maxN := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		m := planNodeIDRE.FindStringSubmatch(base)
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		if n > maxN {
			maxN = n
		}
	}
	return fmt.Sprintf("N-%03d_%s", maxN+1, planNodeSlug(title)), nil
}

func yamlScalar(v string) string {
	return strconv.Quote(strings.TrimSpace(v))
}

func renderPlanNode(n planNode) []byte {
	var b strings.Builder
	b.WriteString("---\n")
	for _, kv := range [][2]string{
		{"id", n.ID}, {"title", n.Title}, {"parent", n.Parent}, {"trigger", n.Trigger},
		{"owner", n.Owner}, {"done_when", n.DoneWhen}, {"status", n.Status},
		{"return_to", n.ReturnTo}, {"created_at", n.CreatedAt}, {"updated_at", n.UpdatedAt},
	} {
		fmt.Fprintf(&b, "%s: %s\n", kv[0], yamlScalar(kv[1]))
	}
	b.WriteString("receipts:\n")
	for _, receipt := range n.Receipts {
		fmt.Fprintf(&b, "  - %s\n", yamlScalar(receipt))
	}
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "# %s\n\n", n.Title)
	fmt.Fprintf(&b, "- Родитель нити: %s\n", n.Parent)
	fmt.Fprintf(&b, "- Владелец: %s\n", n.Owner)
	fmt.Fprintf(&b, "- Готово когда: %s\n", n.DoneWhen)
	return []byte(b.String())
}

func parseYAMLScalar(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, string(rune(34))) {
		if s, err := strconv.Unquote(v); err == nil {
			return s
		}
	}
	return strings.Trim(v, "'")
}

func readPlanNode(path string) (planNode, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return planNode{}, err
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return planNode{}, fmt.Errorf("node has no YAML header: %s", path)
	}
	n := planNode{Path: path}
	inReceipts := false
	closed := false
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "---" {
			closed = true
			break
		}
		if inReceipts && strings.HasPrefix(strings.TrimSpace(line), "- ") {
			n.Receipts = append(n.Receipts, parseYAMLScalar(strings.TrimSpace(line)[2:]))
			continue
		}
		inReceipts = false
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), parseYAMLScalar(v)
		switch k {
		case "id":
			n.ID = v
		case "title":
			n.Title = v
		case "parent":
			n.Parent = v
		case "trigger":
			n.Trigger = v
		case "owner":
			n.Owner = v
		case "done_when":
			n.DoneWhen = v
		case "status":
			n.Status = v
		case "return_to":
			n.ReturnTo = v
		case "created_at":
			n.CreatedAt = v
		case "updated_at":
			n.UpdatedAt = v
		case "receipts":
			inReceipts = true
		}
	}
	if !closed {
		return planNode{}, fmt.Errorf("node YAML header is not closed: %s", path)
	}
	if err := validatePlanNode(n); err != nil {
		return planNode{}, fmt.Errorf("%s: %w", path, err)
	}
	return n, nil
}

func validatePlanNode(n planNode) error {
	if planNodeIDRE.FindStringSubmatch(strings.TrimSpace(n.ID)) == nil {
		return fmt.Errorf("invalid node id %q", n.ID)
	}
	for name, value := range map[string]string{
		"title": n.Title, "parent": n.Parent, "trigger": n.Trigger,
		"done_when": n.DoneWhen, "return_to": n.ReturnTo,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	if n.Status != "open" && n.Status != "blocked" && n.Status != "closed" {
		return fmt.Errorf("invalid status %q", n.Status)
	}
	for name, value := range map[string]string{"created_at": n.CreatedAt, "updated_at": n.UpdatedAt} {
		if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
			return fmt.Errorf("invalid %s: %w", name, err)
		}
	}
	return nil
}

func planNodePathByID(root, id string) (string, error) {
	id = strings.TrimSuffix(strings.TrimSpace(id), ".md")
	if planNodeIDRE.MatchString(id) {
		path := filepath.Join(planNodeDir(root), id+".md")
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			return path, nil
		}
	}
	if strings.HasPrefix(id, "N-") && !strings.Contains(id, "_") {
		entries, _ := os.ReadDir(planNodeDir(root))
		var hits []string
		for _, e := range entries {
			if !e.IsDir() && strings.HasPrefix(e.Name(), id+"_") && strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
				hits = append(hits, filepath.Join(planNodeDir(root), e.Name()))
			}
		}
		if len(hits) == 1 {
			return hits[0], nil
		}
		if len(hits) > 1 {
			return "", fmt.Errorf("node id %s is ambiguous", id)
		}
	}
	return "", fmt.Errorf("node not found: %s", id)
}

func listPlanNodes(root string) ([]planNode, error) {
	entries, err := os.ReadDir(planNodeDir(root))
	if os.IsNotExist(err) {
		return []planNode{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out []planNode
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "N-") || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			continue
		}
		n, err := readPlanNode(filepath.Join(planNodeDir(root), e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func spineNodeLine(n planNode) string {
	owner := strings.TrimSpace(n.Owner)
	if owner == "" {
		owner = "—"
	}
	return fmt.Sprintf("- [%s] %s · owner:%s · [[%s]] · %s", n.Status, n.Parent, owner, n.ID, n.Title)
}

func addNodeToSpine(planPath string, n planNode) error {
	raw, err := os.ReadFile(planPath)
	if err != nil {
		return err
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	line := spineNodeLine(n)
	if strings.Contains(text, "[["+n.ID+"]]") {
		return fmt.Errorf("PLAN.md already links %s", n.ID)
	}
	if !strings.Contains(text, planNodeMarker) {
		text = strings.TrimRight(text, "\n") + "\n\n## План: нить и узлы\n\n" + planNodeMarker + "\n"
	}
	text = strings.TrimRight(text, "\n") + "\n" + line + "\n"
	return writeFileAtomic(planPath, []byte(text))
}

func updateNodeInSpine(planPath string, n planNode) error {
	raw, err := os.ReadFile(planPath)
	if err != nil {
		return err
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	found := false
	for i, line := range lines {
		if strings.Contains(line, "[["+n.ID+"]]") {
			lines[i] = spineNodeLine(n)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("PLAN.md has no link to %s", n.ID)
	}
	return writeFileAtomic(planPath, []byte(strings.Join(lines, "\n")))
}

func cmdPlanNode(argv []string) int {
	if len(argv) == 0 {
		fmt.Fprintln(os.Stderr, "usage: air-worker plan node <new|close|list>")
		return 2
	}
	switch argv[0] {
	case "new":
		return cmdPlanNodeNew(argv[1:])
	case "close":
		return cmdPlanNodeClose(argv[1:])
	case "list":
		return cmdPlanNodeList(argv[1:])
	default:
		fmt.Fprintln(os.Stderr, "unknown plan node command:", argv[0])
		return 2
	}
}

func cmdPlanNodeNew(argv []string) int {
	fs := flag.NewFlagSet("plan node new", flag.ContinueOnError)
	product := fs.String("product", ".", "product root")
	title := fs.String("title", "", "node title")
	parent := fs.String("parent", "", "spine parent/stage")
	owner := fs.String("owner", "", "owner session/window")
	doneWhen := fs.String("done-when", "", "closure criterion")
	trigger := fs.String("trigger", "", "LPR wording / trigger")
	returnTo := fs.String("return-to", "", "where to return in the spine")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	root, err := normalizePlanNodeProduct(*product)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	for name, value := range map[string]string{"title": *title, "parent": *parent, "owner": *owner, "done-when": *doneWhen} {
		if strings.TrimSpace(value) == "" {
			fmt.Fprintf(os.Stderr, "-%s is required%s", name, lineEnding)
			return 2
		}
	}
	id, err := nextPlanNodeID(root, *title)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	now := planNodeNow().UTC().Format(time.RFC3339Nano)
	trig := strings.TrimSpace(*trigger)
	if trig == "" {
		trig = strings.TrimSpace(*title) + " @ " + now
	}
	ret := strings.TrimSpace(*returnTo)
	if ret == "" {
		ret = strings.TrimSpace(*parent)
	}
	n := planNode{
		ID: id, Title: strings.TrimSpace(*title), Parent: strings.TrimSpace(*parent),
		Trigger: trig, Owner: strings.TrimSpace(*owner), DoneWhen: strings.TrimSpace(*doneWhen),
		Status: "open", ReturnTo: ret, Receipts: []string{}, CreatedAt: now, UpdatedAt: now,
	}
	if err := validatePlanNode(n); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := os.MkdirAll(planNodeDir(root), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	nodePath := filepath.Join(planNodeDir(root), n.ID+".md")
	if _, err := os.Stat(nodePath); err == nil {
		fmt.Fprintln(os.Stderr, "node already exists:", nodePath)
		return 2
	}
	if err := writeFileAtomic(nodePath, renderPlanNode(n)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := addNodeToSpine(filepath.Join(root, "PLAN.md"), n); err != nil {
		_ = os.Remove(nodePath)
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Printf("%s %s%s", n.ID, nodePath, lineEnding)
	return 0
}

func cmdPlanNodeClose(argv []string) int {
	if len(argv) == 0 || strings.HasPrefix(argv[0], "-") {
		fmt.Fprintln(os.Stderr, "usage: air-worker plan node close <id> -receipt <path/ref> [-product <root>]")
		return 2
	}
	id := argv[0]
	fs := flag.NewFlagSet("plan node close", flag.ContinueOnError)
	product := fs.String("product", ".", "product root")
	receipt := fs.String("receipt", "", "receipt path/ref")
	if err := fs.Parse(argv[1:]); err != nil {
		return 2
	}
	if strings.TrimSpace(*receipt) == "" {
		fmt.Fprintln(os.Stderr, "-receipt is required")
		return 2
	}
	root, err := normalizePlanNodeProduct(*product)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	path, err := planNodePathByID(root, id)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	nodeBefore, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	planPath := filepath.Join(root, "PLAN.md")
	planBefore, err := os.ReadFile(planPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	n, err := readPlanNode(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if !strings.Contains(string(planBefore), "[["+n.ID+"]]") {
		fmt.Fprintln(os.Stderr, "PLAN.md has no node link:", n.ID)
		return 2
	}
	if n.Status == "closed" {
		fmt.Printf("%s already closed%s", n.ID, lineEnding)
		return 0
	}
	n.Status = "closed"
	n.UpdatedAt = planNodeNow().UTC().Format(time.RFC3339Nano)
	foundReceipt := false
	for _, existing := range n.Receipts {
		if existing == strings.TrimSpace(*receipt) {
			foundReceipt = true
		}
	}
	if !foundReceipt {
		n.Receipts = append(n.Receipts, strings.TrimSpace(*receipt))
	}
	if err := writeFileAtomic(path, renderPlanNode(n)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := updateNodeInSpine(planPath, n); err != nil {
		_ = writeFileAtomic(path, nodeBefore)
		_ = writeFileAtomic(planPath, planBefore)
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Printf("%s closed -> %s%s", n.ID, n.ReturnTo, lineEnding)
	return 0
}

func cmdPlanNodeList(argv []string) int {
	fs := flag.NewFlagSet("plan node list", flag.ContinueOnError)
	product := fs.String("product", ".", "product root")
	openOnly := fs.Bool("open", false, "only open nodes")
	noOwner := fs.Bool("no-owner", false, "only nodes without owner")
	stale := fs.Duration("stale", 0, "only open nodes without movement for duration, e.g. 24h")
	asJSON := fs.Bool("json", false, "machine-readable JSON")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	root, err := normalizePlanNodeProduct(*product)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	nodes, err := listPlanNodes(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	now := planNodeNow().UTC()
	out := make([]planNode, 0, len(nodes))
	for _, n := range nodes {
		if *openOnly && n.Status != "open" {
			continue
		}
		if *noOwner && strings.TrimSpace(n.Owner) != "" {
			continue
		}
		if *stale > 0 {
			if n.Status != "open" {
				continue
			}
			t, _ := time.Parse(time.RFC3339Nano, n.UpdatedAt)
			if now.Sub(t) < *stale {
				continue
			}
		}
		out = append(out, n)
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return 0
	}
	for _, n := range out {
		fmt.Printf("%s %-7s owner=%s parent=%s updated=%s %s%s",
			n.ID, n.Status, n.Owner, n.Parent, n.UpdatedAt, n.Title, lineEnding)
	}
	return 0
}

func cmdPlanSpine(argv []string) int {
	fs := flag.NewFlagSet("plan spine", flag.ContinueOnError)
	product := fs.String("product", ".", "product root")
	asJSON := fs.Bool("json", false, "machine-readable JSON")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	root, err := normalizePlanNodeProduct(*product)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	nodes, err := listPlanNodes(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if *asJSON {
		var open []planNode
		for _, n := range nodes {
			if n.Status != "closed" {
				open = append(open, n)
			}
		}
		doc := struct {
			Product string     `json:"product"`
			Plan    string     `json:"plan"`
			Open    []planNode `json:"open_nodes"`
		}{Product: root, Plan: filepath.Join(root, "PLAN.md"), Open: open}
		_ = json.NewEncoder(os.Stdout).Encode(doc)
		return 0
	}
	fmt.Printf("PLAN: %s%s", filepath.Join(root, "PLAN.md"), lineEnding)
	openCount := 0
	for _, n := range nodes {
		if n.Status == "closed" {
			continue
		}
		openCount++
		fmt.Printf("%s %s · owner:%s · [[%s]] · %s%s", n.Status, n.Parent, n.Owner, n.ID, n.Title, lineEnding)
	}
	if openCount == 0 {
		fmt.Print("открытых узлов нет" + lineEnding)
	}
	return 0
}

func cmdPlanMigrate(argv []string) int {
	fmt.Fprintln(os.Stderr, "plan migrate is not implemented in this intermediate slice")
	return 2
}

var errPlanNodeNotFound = errors.New("plan node not found")
