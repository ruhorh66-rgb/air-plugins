package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// New and migrate have no pre-existing node identity. Requiring an explicit
// request ID is the only unambiguous distinction between a retry and a second
// intentional creation with exactly the same title/criteria.
type sharedPlanCreateRequest struct {
	ID        string `json:"request_id"`
	Operation string `json:"operation"`
	Title     string `json:"title"`
	Parent    string `json:"parent"`
	Owner     string `json:"owner"`
	DoneWhen  string `json:"done_when"`
	Trigger   string `json:"trigger"`
	ReturnTo  string `json:"return_to"`
}

type sharedPlanCreateNode struct {
	Node    planNode `json:"node"`
	Content []byte   `json:"content"`
	SHA     string   `json:"sha256"`
	Ref     string   `json:"reference"`
}

type sharedPlanCreateIntent struct {
	Schema        string                  `json:"schema"`
	Product       string                  `json:"product"`
	ProductID     string                  `json:"product_id"`
	RuntimeRoot   string                  `json:"runtime_root"`
	Request       sharedPlanCreateRequest `json:"request"`
	RequestSHA    string                  `json:"request_sha256"`
	Actor         string                  `json:"actor"`
	Phase         string                  `json:"phase"`
	BeforePlanSHA string                  `json:"before_plan_sha256"`
	AfterPlan     []byte                  `json:"after_plan,omitempty"`
	Nodes         []sharedPlanCreateNode  `json:"nodes"`
}

func persistSharedPlanCreate(path string, intent sharedPlanCreateIntent) error {
	b, err := json.MarshalIndent(intent, "", "  ")
	if err != nil {
		return err
	}
	if len(b) > 8*sharedLearningMaxBytes {
		return errors.New("plan create intent exceeds byte limit")
	}
	return writeFileAtomicDurable(path, append(b, '\n'))
}

// Publish a new node without replacing an editor's concurrent file. A temporary
// same-directory file is linked atomically and then unlinked; a crash cannot
// expose a half-written node. Filesystems without hard links fail explicitly.
func publishNewPlanNode(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".plan-publish-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(content); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Link(name, path); err != nil {
		return fmt.Errorf("publish new node without replacement: %w", err)
	}
	return nil
}

func cmdSharedPlanCreate(root string, s sharedLearningSettings, req sharedPlanCreateRequest, actor string, asJSON bool) int {
	intentPath := ""
	fail := func(err error) int {
		if asJSON {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"schema": "air-worker.plan.node.mutation/v1", "action": "pending", "request_id": req.ID, "error": err.Error(), "recovery_ref": intentPath})
		} else {
			fmt.Fprintln(os.Stderr, "shared plan mutation:", err)
		}
		return 2
	}
	if strings.TrimSpace(req.ID) == "" || len(req.ID) > 256 {
		return fail(errors.New("shared new/migrate requires a stable -request-id of 1..256 bytes"))
	}
	if req.Operation != "new" && req.Operation != "migrate" {
		return fail(errors.New("unsupported shared plan operation"))
	}
	if strings.TrimSpace(req.Owner) == "" || actor == "" {
		return fail(errors.New("shared plan owner and actor are required"))
	}
	requestBytes, _ := json.Marshal(req)
	requestSHA := learnSHA(requestBytes)
	intentPath = filepath.Join(s.RuntimeRoot, "plan-create", learnSHA([]byte(req.Operation+"\n"+req.ID))+".json")
	if err := rejectConflictingSharedPlanIntent(root, s, intentPath); err != nil {
		return fail(err)
	}
	var intent sharedPlanCreateIntent
	b, err := readLearningBounded(intentPath, 8*sharedLearningMaxBytes)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fail(err)
	}
	if errors.Is(err, os.ErrNotExist) {
		before, err := os.ReadFile(filepath.Join(root, "PLAN.md"))
		if err != nil {
			return fail(err)
		}
		if err := sharedPlanPreflight(root, s); err != nil {
			return fail(err)
		}
		intent = sharedPlanCreateIntent{Schema: "air-worker.plan.create-intent/v1", Product: root, ProductID: s.ProductID, RuntimeRoot: s.RuntimeRoot, Request: req, RequestSHA: requestSHA, Actor: actor, Phase: "prepared", BeforePlanSHA: learnSHA(before)}
		now := planNodeNow().UTC().Format(time.RFC3339Nano)
		trigger := req.Trigger
		if trigger == "" {
			trigger = req.Operation + " @ " + now
		}
		if req.Operation == "new" {
			id, err := nextPlanNodeID(root, req.Title)
			if err != nil {
				return fail(err)
			}
			ret := req.ReturnTo
			if ret == "" {
				ret = req.Parent
			}
			n := planNode{ID: id, Title: req.Title, Parent: req.Parent, Owner: req.Owner, DoneWhen: req.DoneWhen, Trigger: trigger, Status: "open", ReturnTo: ret, Receipts: []string{}, CreatedAt: now, UpdatedAt: now}
			if err := validatePlanNode(n); err != nil {
				return fail(err)
			}
			content := renderPlanNode(n)
			intent.Nodes = []sharedPlanCreateNode{{Node: n, Content: content, SHA: learnSHA(content), Ref: filepath.ToSlash(filepath.Join(planNodeDirName, id+".md"))}}
		} else {
			if strings.Contains(string(before), planNodeMarker) {
				return fail(errors.New("PLAN.md is already migrated to node spine"))
			}
			entries, err := os.ReadDir(planNodeDir(root))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return fail(err)
			}
			for _, e := range entries {
				if !e.IsDir() && strings.HasPrefix(e.Name(), "N-") && strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
					return fail(errors.New("migration refuses a populated node directory"))
				}
			}
			preamble, sections, err := splitPlanForMigration(before)
			if err != nil {
				return fail(err)
			}
			var nodes []planNode
			for i, section := range sections {
				n := planNode{ID: fmt.Sprintf("N-%03d_%s", i+1, planNodeSlug(section.Title)), Title: section.Title, Parent: section.Title, Owner: req.Owner, DoneWhen: "review migrated section and close with receipt", Trigger: trigger, Status: "open", ReturnTo: section.Title, Receipts: []string{}, CreatedAt: now, UpdatedAt: now}
				if err := validatePlanNode(n); err != nil {
					return fail(err)
				}
				content := renderPlanNodeWithBody(n, section.Body)
				nodes = append(nodes, n)
				intent.Nodes = append(intent.Nodes, sharedPlanCreateNode{Node: n, Content: content, SHA: learnSHA(content), Ref: section.Title})
			}
			intent.AfterPlan = renderMigratedSpine(preamble, nodes)
		}
		current, err := os.ReadFile(filepath.Join(root, "PLAN.md"))
		if err != nil {
			return fail(err)
		}
		if learnSHA(current) != intent.BeforePlanSHA {
			return fail(errors.New("PLAN changed during creation preflight; edit retained"))
		}
		if err := os.MkdirAll(filepath.Dir(intentPath), 0700); err != nil {
			return fail(err)
		}
		if err := persistSharedPlanCreate(intentPath, intent); err != nil {
			return fail(err)
		}
	} else if err := json.Unmarshal(b, &intent); err != nil {
		return fail(err)
	}
	storedRequest, _ := json.Marshal(intent.Request)
	if intent.Schema != "air-worker.plan.create-intent/v1" || intent.Product != root || intent.ProductID != s.ProductID || intent.RuntimeRoot != s.RuntimeRoot || intent.RequestSHA != requestSHA || learnSHA(storedRequest) != requestSHA || intent.Actor == "" || (intent.Phase != "prepared" && intent.Phase != "complete") || len(intent.Nodes) == 0 {
		return fail(errors.New("plan request identity/content conflict; original intent retained"))
	}
	seen := map[string]bool{}
	for _, entry := range intent.Nodes {
		if err := validatePlanNode(entry.Node); err != nil {
			return fail(err)
		}
		if seen[entry.Node.ID] || entry.Node.Status != "open" || learnSHA(entry.Content) != entry.SHA {
			return fail(errors.New("invalid plan intent node snapshot"))
		}
		seen[entry.Node.ID] = true
		body, err := parsePlanNodeBody("intent", entry.Content)
		if err != nil {
			return fail(err)
		}
		if learnSHA(renderPlanNodeWithBody(entry.Node, body)) != entry.SHA {
			return fail(errors.New("plan intent metadata and byte snapshot disagree"))
		}
	}
	planPath := filepath.Join(root, "PLAN.md")
	if intent.Phase != "complete" {
		// Check every existing node before publishing any missing member of a batch.
		for _, entry := range intent.Nodes {
			path := filepath.Join(planNodeDir(root), entry.Node.ID+".md")
			info, err := os.Lstat(path)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return fail(err)
			}
			if err == nil {
				if !info.Mode().IsRegular() {
					return fail(errors.New("pending node path is not a regular file"))
				}
				current, err := os.ReadFile(path)
				if err != nil {
					return fail(err)
				}
				if learnSHA(current) != entry.SHA {
					return fail(errors.New("pending node changed externally; refusing overwrite"))
				}
			}
		}
		if req.Operation == "migrate" {
			current, err := os.ReadFile(planPath)
			if err != nil {
				return fail(err)
			}
			sha := learnSHA(current)
			if sha != intent.BeforePlanSHA && sha != learnSHA(intent.AfterPlan) {
				return fail(errors.New("pending migration PLAN changed externally; edit retained"))
			}
		}
		for _, entry := range intent.Nodes {
			path := filepath.Join(planNodeDir(root), entry.Node.ID+".md")
			if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
				if err := publishNewPlanNode(path, entry.Content); err != nil {
					return fail(err)
				}
			} else if err != nil {
				return fail(err)
			}
		}
		if req.Operation == "migrate" {
			current, err := os.ReadFile(planPath)
			if err != nil {
				return fail(err)
			}
			if learnSHA(current) == intent.BeforePlanSHA {
				if err := writeFileAtomic(planPath, intent.AfterPlan); err != nil {
					return fail(err)
				}
			} else if learnSHA(current) != learnSHA(intent.AfterPlan) {
				return fail(errors.New("migration PLAN changed before publication"))
			}
		} else {
			n := intent.Nodes[0].Node
			current, err := os.ReadFile(planPath)
			if err != nil {
				return fail(err)
			}
			if strings.Contains(string(current), "[["+n.ID+"]]") {
				found := false
				for _, line := range strings.Split(strings.ReplaceAll(string(current), "\r\n", "\n"), "\n") {
					if line == spineNodeLine(n) {
						found = true
					}
				}
				if !found {
					return fail(errors.New("existing new-node spine link was changed externally"))
				}
			} else if err := addNodeToSpine(planPath, n); err != nil {
				return fail(err)
			}
		}
		var events []map[string]any
		for _, entry := range intent.Nodes {
			at, err := time.Parse(time.RFC3339Nano, entry.Node.CreatedAt)
			if err != nil {
				return fail(err)
			}
			action, source := "created", "plan-node"
			if req.Operation == "migrate" {
				action, source = "migrated", "plan-migrate"
			}
			events = append(events, planNodeEventRowWithActor(entry.Node, action, entry.Ref, source, at, intent.Actor))
		}
		if err := appendPlanNodeEvents(root, events); err != nil {
			return fail(err)
		}
		if err := sharedPlanPreflight(root, s); err != nil {
			return fail(err)
		}
		intent.Phase = "complete"
		if err := persistSharedPlanCreate(intentPath, intent); err != nil {
			return fail(err)
		}
	}
	var currentNodes []planNode
	plan, err := os.ReadFile(planPath)
	if err != nil {
		return fail(err)
	}
	for _, entry := range intent.Nodes {
		n, err := readPlanNode(filepath.Join(planNodeDir(root), entry.Node.ID+".md"))
		if err != nil {
			return fail(err)
		}
		if n.ID != entry.Node.ID || n.CreatedAt != entry.Node.CreatedAt || !strings.Contains(string(plan), "[["+n.ID+"]]") {
			return fail(errors.New("completed plan intent has external identity/link drift"))
		}
		currentNodes = append(currentNodes, n)
	}
	action := "created"
	if req.Operation == "migrate" {
		action = "migrated"
	}
	if asJSON {
		result := map[string]any{"schema": "air-worker.plan.node.mutation/v1", "action": action, "request_id": req.ID, "actor": intent.Actor, "recovery_ref": intentPath, "nodes": currentNodes}
		if len(currentNodes) == 1 {
			result["node"], result["path"] = currentNodes[0], currentNodes[0].Path
		}
		if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
			return 2
		}
	} else {
		fmt.Printf("%s %d nodes; request=%s%s", action, len(currentNodes), req.ID, lineEnding)
	}
	return 0
}
