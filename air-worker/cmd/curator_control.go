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
	"sort"
	"strings"
	"time"
)

const curatorControlSchema = "air-worker.curator-control/v1"

type curatorPeer struct {
	ID           string `json:"id"`
	Provider     string `json:"provider"`
	Model        string `json:"model,omitempty"`
	Enabled      bool   `json:"enabled"`
	MaxActive    int    `json:"max_active"`
	RegisteredAt string `json:"registered_at"`
	UpdatedAt    string `json:"updated_at"`
}

type curatorAssignment struct {
	ID        string `json:"id"`
	PeerID    string `json:"peer_id"`
	Product   string `json:"product,omitempty"`
	ProfileID string `json:"profile_id"`
	SessionID string `json:"session_id"`
	RunID     string `json:"run_id"`
	State     string `json:"state"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type curatorControlState struct {
	Schema      string              `json:"schema"`
	Peers       []curatorPeer       `json:"peers"`
	Assignments []curatorAssignment `json:"assignments"`
}

func curatorControlDir(override string) string {
	base := strings.TrimSpace(override)
	if base == "" {
		base = sessionStateDir()
	}
	return filepath.Join(base, "curator-v1")
}

func curatorControlPath(dir string) string { return filepath.Join(dir, "control.json") }

func readCuratorControl(dir string) (curatorControlState, error) {
	var state curatorControlState
	err := readJSON(curatorControlPath(dir), &state)
	if errors.Is(err, os.ErrNotExist) {
		return curatorControlState{Schema: curatorControlSchema, Peers: []curatorPeer{}, Assignments: []curatorAssignment{}}, nil
	}
	if err != nil {
		return curatorControlState{}, err
	}
	if state.Schema != curatorControlSchema {
		return curatorControlState{}, fmt.Errorf("curator control schema %q, expected %q", state.Schema, curatorControlSchema)
	}
	if state.Peers == nil {
		state.Peers = []curatorPeer{}
	}
	if state.Assignments == nil {
		state.Assignments = []curatorAssignment{}
	}
	if err := validateCuratorControlState(state); err != nil {
		return curatorControlState{}, err
	}
	return state, nil
}

func validateCuratorControlState(state curatorControlState) error {
	peerIDs := make(map[string]struct{}, len(state.Peers))
	for i, peer := range state.Peers {
		id, err := sanitizeIdentityPart("peer-id", peer.ID)
		if err != nil || id != peer.ID {
			return fmt.Errorf("curator peer[%d] invalid id %q", i, peer.ID)
		}
		provider, err := sanitizeIdentityPart("provider", peer.Provider)
		if err != nil || provider != peer.Provider {
			return fmt.Errorf("curator peer[%d] invalid provider %q", i, peer.Provider)
		}
		if strings.ContainsAny(peer.Model, "\r\n\x00") || len([]rune(peer.Model)) > 128 {
			return fmt.Errorf("curator peer[%d] invalid model", i)
		}
		if peer.MaxActive < 1 || peer.MaxActive > 32 {
			return fmt.Errorf("curator peer[%d] max_active=%d outside 1..32", i, peer.MaxActive)
		}
		if _, exists := peerIDs[peer.ID]; exists {
			return fmt.Errorf("duplicate curator peer id %q", peer.ID)
		}
		peerIDs[peer.ID] = struct{}{}
	}
	assignmentIDs := make(map[string]struct{}, len(state.Assignments))
	activeScopes := make(map[string]string)
	activeByPeer := make(map[string]int)
	for i, a := range state.Assignments {
		identityParts := []struct {
			name  string
			value string
		}{
			{name: "id", value: a.ID},
			{name: "peer", value: a.PeerID},
			{name: "profile", value: a.ProfileID},
			{name: "session", value: a.SessionID},
			{name: "run", value: a.RunID},
		}
		for _, part := range identityParts {
			clean, err := sanitizeIdentityPart(part.name, part.value)
			if err != nil || clean != part.value {
				return fmt.Errorf("curator assignment[%d] invalid %s %q", i, part.name, part.value)
			}
		}
		if _, exists := assignmentIDs[a.ID]; exists {
			return fmt.Errorf("duplicate curator assignment id %q", a.ID)
		}
		assignmentIDs[a.ID] = struct{}{}
		if _, exists := peerIDs[a.PeerID]; !exists {
			return fmt.Errorf("curator assignment[%d] references unknown peer %q", i, a.PeerID)
		}
		switch a.State {
		case "active", "paused", "degraded", "revoked", "closed":
		default:
			return fmt.Errorf("curator assignment[%d] invalid state %q", i, a.State)
		}
		if curatorAssignmentActive(a) {
			scope := strings.Join([]string{a.Product, a.ProfileID, a.SessionID, a.RunID}, "\x1f")
			if prior, exists := activeScopes[scope]; exists {
				return fmt.Errorf("curator scope has multiple active assignments %q and %q", prior, a.ID)
			}
			activeScopes[scope] = a.ID
			activeByPeer[a.PeerID]++
		}
	}
	for _, peer := range state.Peers {
		if activeByPeer[peer.ID] > peer.MaxActive {
			return fmt.Errorf("curator peer %q active assignments %d exceed max_active=%d", peer.ID, activeByPeer[peer.ID], peer.MaxActive)
		}
	}
	return nil
}

func writeCuratorControl(dir string, state curatorControlState) error {
	state.Schema = curatorControlSchema
	if err := validateCuratorControlState(state); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	sort.SliceStable(state.Peers, func(i, j int) bool { return state.Peers[i].ID < state.Peers[j].ID })
	sort.SliceStable(state.Assignments, func(i, j int) bool { return state.Assignments[i].ID < state.Assignments[j].ID })
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(curatorControlPath(dir), append(raw, '\n'))
}

func curatorStateLock(dir string) (*osLock, error) {
	lock, ok := acquireLock(lockName("curator-state", dir))
	if !ok {
		return nil, errors.New("curator state is busy in another process")
	}
	return lock, nil
}

func newCuratorID(prefix string) (string, error) {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s-%s", prefix, time.Now().UTC().Format("20060102T150405Z"), hex.EncodeToString(suffix[:])), nil
}

func curatorPeerIndex(state curatorControlState, id string) int {
	for i := range state.Peers {
		if state.Peers[i].ID == id {
			return i
		}
	}
	return -1
}

func curatorAssignmentIndex(state curatorControlState, id string) int {
	for i := range state.Assignments {
		if state.Assignments[i].ID == id {
			return i
		}
	}
	return -1
}

func curatorAssignmentActive(a curatorAssignment) bool {
	return a.State == "active" || a.State == "paused" || a.State == "degraded"
}

func curatorScopeEqual(a curatorAssignment, product, profile, session, run string) bool {
	return a.Product == product && a.ProfileID == profile && a.SessionID == session && a.RunID == run
}

func cmdCuratorPeer(argv []string) int {
	if len(argv) == 0 {
		fmt.Fprintln(os.Stderr, "usage: air-worker curator peer register|list ...")
		return 2
	}
	switch argv[0] {
	case "register":
		return cmdCuratorPeerRegister(argv[1:])
	case "list":
		return cmdCuratorPeerList(argv[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown curator peer action %q\n", argv[0])
		return 2
	}
}

func cmdCuratorPeerRegister(argv []string) int {
	fs := flag.NewFlagSet("curator peer register", flag.ContinueOnError)
	idRaw := fs.String("id", "", "stable peer id")
	providerRaw := fs.String("provider", "", "provider identity")
	model := fs.String("model", "", "provider model")
	maxActive := fs.Int("max-active", 1, "maximum simultaneous active assignments for this peer")
	enabled := fs.Bool("enabled", true, "peer may receive assignments")
	stateDir := fs.String("state-dir", "", "override machine state dir")
	asJSON := fs.Bool("json", false, "machine output")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	id, err := sanitizeIdentityPart("id", *idRaw)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	provider, err := sanitizeIdentityPart("provider", *providerRaw)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if *maxActive < 1 || *maxActive > 32 {
		fmt.Fprintln(os.Stderr, "-max-active must be in 1..32")
		return 2
	}
	dir := curatorControlDir(*stateDir)
	lock, err := curatorStateLock(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer lock.release()
	state, err := readCuratorControl(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	peer := curatorPeer{
		ID: id, Provider: provider, Model: strings.TrimSpace(*model), Enabled: *enabled,
		MaxActive: *maxActive, RegisteredAt: now, UpdatedAt: now,
	}
	if idx := curatorPeerIndex(state, id); idx >= 0 {
		peer.RegisteredAt = state.Peers[idx].RegisteredAt
		state.Peers[idx] = peer
	} else {
		state.Peers = append(state.Peers, peer)
	}
	if err := writeCuratorControl(dir, state); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if *asJSON {
		b, _ := json.Marshal(peer)
		fmt.Println(string(b))
	} else {
		fmt.Printf("curator peer %s: provider=%s model=%s enabled=%t max_active=%d\n",
			peer.ID, peer.Provider, peer.Model, peer.Enabled, peer.MaxActive)
	}
	return 0
}

func cmdCuratorPeerList(argv []string) int {
	fs := flag.NewFlagSet("curator peer list", flag.ContinueOnError)
	stateDir := fs.String("state-dir", "", "override machine state dir")
	asJSON := fs.Bool("json", false, "machine output")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	state, err := readCuratorControl(curatorControlDir(*stateDir))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if *asJSON {
		b, _ := json.Marshal(map[string]any{"schema": curatorControlSchema, "peers": state.Peers})
		fmt.Println(string(b))
		return 0
	}
	for _, peer := range state.Peers {
		fmt.Printf("%s | provider=%s | model=%s | enabled=%t | max_active=%d\n",
			peer.ID, peer.Provider, peer.Model, peer.Enabled, peer.MaxActive)
	}
	return 0
}

func cmdCuratorAssignment(argv []string) int {
	if len(argv) == 0 {
		fmt.Fprintln(os.Stderr, "usage: air-worker curator assignment assign|list|revoke ...")
		return 2
	}
	switch argv[0] {
	case "assign":
		return cmdCuratorAssign(argv[1:])
	case "list":
		return cmdCuratorAssignmentList(argv[1:])
	case "revoke":
		return cmdCuratorAssignmentRevoke(argv[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown curator assignment action %q\n", argv[0])
		return 2
	}
}

func curatorAssignmentFlags(name string) (*flag.FlagSet, *string, *string, *string, *string, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	peer := fs.String("peer", "", "registered curator peer id")
	profile := fs.String("profile", "", "profile id")
	session := fs.String("session", "", "session id")
	run := fs.String("run", "", "run id")
	stateDir := fs.String("state-dir", "", "override machine state dir")
	return fs, peer, profile, session, run, stateDir
}

func cmdCuratorAssign(argv []string) int {
	fs, peerRaw, profileRaw, sessionRaw, runRaw, stateDir := curatorAssignmentFlags("curator assignment assign")
	product := fs.String("product", "", "optional managed product root")
	asJSON := fs.Bool("json", false, "machine output")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	peerID, err := sanitizeIdentityPart("peer", *peerRaw)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	profile, err := sanitizeIdentityPart("profile", *profileRaw)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	session, err := sanitizeIdentityPart("session", *sessionRaw)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	runID, err := sanitizeIdentityPart("run", *runRaw)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	productPath := ""
	if strings.TrimSpace(*product) != "" {
		productPath, err = filepath.Abs(*product)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		if st, statErr := os.Stat(productPath); statErr != nil || !st.IsDir() {
			fmt.Fprintln(os.Stderr, "product directory not found:", productPath)
			return 2
		}
	}
	dir := curatorControlDir(*stateDir)
	lock, err := curatorStateLock(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer lock.release()
	state, err := readCuratorControl(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	peerIdx := curatorPeerIndex(state, peerID)
	if peerIdx < 0 {
		fmt.Fprintln(os.Stderr, "curator peer is not registered:", peerID)
		return 1
	}
	peer := state.Peers[peerIdx]
	if !peer.Enabled {
		fmt.Fprintln(os.Stderr, "curator peer is disabled:", peerID)
		return 1
	}
	for _, a := range state.Assignments {
		if curatorAssignmentActive(a) && curatorScopeEqual(a, productPath, profile, session, runID) {
			if a.PeerID == peerID && a.Product == productPath {
				if *asJSON {
					b, _ := json.Marshal(a)
					fmt.Println(string(b))
				} else {
					fmt.Printf("curator assignment already active: %s peer=%s scope=%s/%s/%s\n", a.ID, a.PeerID, profile, session, runID)
				}
				return 0
			}
			fmt.Fprintf(os.Stderr, "scope already has active curator assignment %s peer=%s\n", a.ID, a.PeerID)
			return 1
		}
	}
	activeForPeer := 0
	for _, a := range state.Assignments {
		if a.PeerID == peerID && curatorAssignmentActive(a) {
			activeForPeer++
		}
	}
	if activeForPeer >= peer.MaxActive {
		fmt.Fprintf(os.Stderr, "curator peer %s active limit reached: %d/%d\n", peerID, activeForPeer, peer.MaxActive)
		return 1
	}
	id, err := newCuratorID("CA")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	a := curatorAssignment{
		ID: id, PeerID: peerID, Product: productPath, ProfileID: profile,
		SessionID: session, RunID: runID, State: "active", CreatedAt: now, UpdatedAt: now,
	}
	state.Assignments = append(state.Assignments, a)
	if err := writeCuratorControl(dir, state); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if *asJSON {
		b, _ := json.Marshal(a)
		fmt.Println(string(b))
	} else {
		fmt.Printf("curator assignment %s: peer=%s scope=%s/%s/%s state=active\n", a.ID, peerID, profile, session, runID)
	}
	return 0
}

func cmdCuratorAssignmentList(argv []string) int {
	fs := flag.NewFlagSet("curator assignment list", flag.ContinueOnError)
	stateDir := fs.String("state-dir", "", "override machine state dir")
	activeOnly := fs.Bool("active", false, "show only active/paused/degraded")
	asJSON := fs.Bool("json", false, "machine output")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	state, err := readCuratorControl(curatorControlDir(*stateDir))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var rows []curatorAssignment
	for _, a := range state.Assignments {
		if *activeOnly && !curatorAssignmentActive(a) {
			continue
		}
		rows = append(rows, a)
	}
	if *asJSON {
		b, _ := json.Marshal(map[string]any{"schema": curatorControlSchema, "assignments": rows})
		fmt.Println(string(b))
		return 0
	}
	for _, a := range rows {
		fmt.Printf("%s | peer=%s | %s/%s/%s | state=%s | product=%s\n",
			a.ID, a.PeerID, a.ProfileID, a.SessionID, a.RunID, a.State, a.Product)
	}
	return 0
}

func cmdCuratorAssignmentRevoke(argv []string) int {
	fs := flag.NewFlagSet("curator assignment revoke", flag.ContinueOnError)
	idRaw := fs.String("id", "", "assignment id")
	stateDir := fs.String("state-dir", "", "override machine state dir")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	id, err := sanitizeIdentityPart("id", *idRaw)
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
	state, err := readCuratorControl(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	idx := curatorAssignmentIndex(state, id)
	if idx < 0 {
		fmt.Fprintln(os.Stderr, "assignment not found:", id)
		return 1
	}
	if state.Assignments[idx].State != "revoked" && state.Assignments[idx].State != "closed" {
		state.Assignments[idx].State = "revoked"
		state.Assignments[idx].UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err := writeCuratorControl(dir, state); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	fmt.Printf("curator assignment %s: revoked\n", id)
	return 0
}
