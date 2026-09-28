package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const curatorWakeSchema = "air-worker.curator-wake/v1"

type curatorWakeHistoryEvent struct {
	At             string `json:"at"`
	StopReason     string `json:"stop_reason,omitempty"`
	WakeTarget     string `json:"wake_target,omitempty"`
	LastProgressAt string `json:"last_progress_at,omitempty"`
}

type orchestrationWakeState struct {
	PID                 int                       `json:"pid,omitempty"`
	Phase               string                    `json:"phase"`
	LastProgressAt      string                    `json:"last_progress_at,omitempty"`
	ResumeAt            string                    `json:"resume_at,omitempty"`
	StopReason          string                    `json:"stop_reason,omitempty"`
	WakeTarget          string                    `json:"wake_target,omitempty"`
	ResumeCmd           string                    `json:"resume_cmd,omitempty"`
	MachineRestartsHour int                       `json:"machine_restarts_hour,omitempty"`
	SameReasonStreak    int                       `json:"same_reason_streak,omitempty"`
	WakeHistory         []curatorWakeHistoryEvent `json:"wake_history,omitempty"`
}

type curatorWakeCard struct {
	Schema     string `json:"schema"`
	Product    string `json:"product"`
	StatePath  string `json:"state_path"`
	Phase      string `json:"phase"`
	StopReason string `json:"stop_reason,omitempty"`
	WakeTarget string `json:"wake_target"`
	Ready      bool   `json:"ready"`
	Action     string `json:"action"`
	Reason     string `json:"reason"`
	ResumeAt   string `json:"resume_at,omitempty"`
	ResumeCmd  string `json:"resume_cmd,omitempty"`
	DryRun     bool   `json:"dry_run"`
}

func orchestrationWakeStatePath(root string) string {
	return filepath.Join(root, ".woody", "orchestration.state.json")
}

func baseWakeTarget(s orchestrationWakeState) string {
	phase := strings.ToLower(strings.TrimSpace(s.Phase))
	reason := strings.ToLower(strings.TrimSpace(s.StopReason))
	switch {
	case phase == "done":
		return "none"
	case phase == "gate" || reason == "gate":
		return "lpr"
	case reason == "not_proven" || reason == "budget" || reason == "sandbox":
		return "session"
	case phase == "limit_wait" || reason == "limit" || reason == "judge_error":
		return "machine"
	case phase == "stopped":
		return "session"
	default:
		target := strings.ToLower(strings.TrimSpace(s.WakeTarget))
		if target == "machine" || target == "session" || target == "lpr" {
			return target
		}
		return "none"
	}
}

func wakeEscalationCounters(s orchestrationWakeState, now time.Time) (int, int, error) {
	machineRestarts := s.MachineRestartsHour
	sameReasonStreak := s.SameReasonStreak
	if machineRestarts < 0 || sameReasonStreak < 0 {
		return 0, 0, errors.New("wake counters cannot be negative")
	}
	if len(s.WakeHistory) == 0 {
		return machineRestarts, sameReasonStreak, nil
	}

	cutoff := now.Add(-time.Hour)
	historyMachineRestarts := 0
	var previousAt time.Time
	for i, event := range s.WakeHistory {
		at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(event.At))
		if err != nil {
			return 0, 0, fmt.Errorf("wake_history[%d] invalid at: %w", i, err)
		}
		if at.After(now) {
			return 0, 0, fmt.Errorf("wake_history[%d] is in the future", i)
		}
		if !previousAt.IsZero() && at.Before(previousAt) {
			return 0, 0, fmt.Errorf("wake_history is not chronological at index %d", i)
		}
		previousAt = at
		target := strings.ToLower(strings.TrimSpace(event.WakeTarget))
		if target != "" && target != "none" && target != "machine" && target != "session" && target != "lpr" {
			return 0, 0, fmt.Errorf("wake_history[%d] invalid wake_target %q", i, event.WakeTarget)
		}
		if target == "machine" && !at.Before(cutoff) {
			historyMachineRestarts++
		}
	}
	if historyMachineRestarts > machineRestarts {
		machineRestarts = historyMachineRestarts
	}

	currentReason := strings.ToLower(strings.TrimSpace(s.StopReason))
	if currentReason != "" {
		currentProgress := strings.TrimSpace(s.LastProgressAt)
		historyStreak := 1 // current stop is the next attempt after the recorded history.
		for i := len(s.WakeHistory) - 1; i >= 0; i-- {
			event := s.WakeHistory[i]
			if strings.ToLower(strings.TrimSpace(event.StopReason)) != currentReason ||
				strings.TrimSpace(event.LastProgressAt) != currentProgress {
				break
			}
			historyStreak++
		}
		if historyStreak > sameReasonStreak {
			sameReasonStreak = historyStreak
		}
	}
	return machineRestarts, sameReasonStreak, nil
}

func applyWakeEscalation(target string, s orchestrationWakeState, now time.Time) (string, string, error) {
	machineRestarts, sameReasonStreak, err := wakeEscalationCounters(s, now)
	if err != nil {
		return "", "", err
	}
	switch target {
	case "machine":
		switch {
		case machineRestarts >= 3 && sameReasonStreak >= 3:
			return "session", "machine restart limit reached (>=3/hour) and same reason repeated >=3; escalate machine->session", nil
		case machineRestarts >= 3:
			return "session", "machine restart limit reached (>=3/hour); escalate machine->session", nil
		case sameReasonStreak >= 3:
			return "session", "same reason repeated >=3 without progress; escalate machine->session", nil
		}
	case "session":
		if sameReasonStreak >= 3 {
			return "lpr", "same reason repeated >=3 without progress; escalate session->lpr", nil
		}
	}
	return target, "", nil
}

func deriveWakeCard(root, statePath string, s orchestrationWakeState, now time.Time) (curatorWakeCard, error) {
	card := curatorWakeCard{
		Schema: curatorWakeSchema, Product: root, StatePath: statePath,
		Phase: strings.TrimSpace(s.Phase), StopReason: strings.TrimSpace(s.StopReason),
		DryRun: true,
	}
	if card.Phase == "" {
		return card, errors.New("orchestration state phase is empty")
	}
	baseTarget := baseWakeTarget(s)
	target, escalation, err := applyWakeEscalation(baseTarget, s, now)
	if err != nil {
		return card, err
	}
	card.WakeTarget = target
	card.ResumeAt = strings.TrimSpace(s.ResumeAt)
	card.ResumeCmd = strings.TrimSpace(s.ResumeCmd)

	declared := strings.ToLower(strings.TrimSpace(s.WakeTarget))
	if declared != "" && declared != "none" && declared != "machine" && declared != "session" && declared != "lpr" {
		return card, fmt.Errorf("invalid declared wake_target %q", s.WakeTarget)
	}
	if declared != "" && declared != "none" && escalation == "" && baseTarget != "none" && declared != baseTarget {
		return card, fmt.Errorf("wake_target %q conflicts with phase/reason derived target %q", declared, baseTarget)
	}

	switch target {
	case "none":
		card.Ready = false
		card.Action = "none"
		card.Reason = "no wake action for current state"
	case "lpr":
		card.Ready = true
		card.Action = "card_only"
		card.Reason = "human/LPR action required; automatic transition forbidden"
	case "session":
		card.Ready = true
		card.Action = "would_notify_session"
		card.Reason = "session owner should be notified; transport is outside this binary slice"
	case "machine":
		if strings.EqualFold(strings.TrimSpace(s.Phase), "limit_wait") || strings.EqualFold(strings.TrimSpace(s.StopReason), "limit") {
			if card.ResumeAt == "" {
				return card, errors.New("limit wake requires resume_at")
			}
			resumeAt, err := time.Parse(time.RFC3339, card.ResumeAt)
			if err != nil {
				return card, fmt.Errorf("invalid resume_at: %w", err)
			}
			if now.Before(resumeAt) {
				card.Ready = false
				card.Action = "wait_until_resume_at"
				card.Reason = "provider limit has not reset yet"
				break
			}
		}
		if card.ResumeCmd == "" {
			return card, errors.New("machine wake requires resume_cmd")
		}
		card.Ready = true
		card.Action = "would_resume_machine"
		card.Reason = "machine wake is eligible, but dry-run never launches resume_cmd"
	default:
		return card, fmt.Errorf("unsupported wake target %q", target)
	}
	if escalation != "" {
		card.Reason += "; " + escalation
	}
	return card, nil
}

func cmdCuratorWake(argv []string) int {
	fs := flag.NewFlagSet("curator wake", flag.ContinueOnError)
	product := fs.String("product", "", "managed product root")
	statePath := fs.String("state", "", "override orchestration state path")
	nowRaw := fs.String("now", "", "override current time RFC3339 for deterministic tests")
	dryRun := fs.Bool("dry-run", true, "calculate only; no process/message side effects")
	asJSON := fs.Bool("json", false, "machine output")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if !*dryRun {
		fmt.Fprintln(os.Stderr, "curator wake: only --dry-run is implemented in 0.10.14 core slice")
		return 2
	}
	if strings.TrimSpace(*product) == "" {
		fmt.Fprintln(os.Stderr, "curator wake: -product is required")
		return 2
	}
	root, err := filepath.Abs(*product)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		fmt.Fprintln(os.Stderr, "product directory not found:", root)
		return 2
	}
	path := strings.TrimSpace(*statePath)
	if path == "" {
		path = orchestrationWakeStatePath(root)
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	var state orchestrationWakeState
	if err := readJSON(path, &state); err != nil {
		fmt.Fprintln(os.Stderr, "curator wake:", err)
		return 2
	}
	now := time.Now().UTC()
	if strings.TrimSpace(*nowRaw) != "" {
		now, err = time.Parse(time.RFC3339, strings.TrimSpace(*nowRaw))
		if err != nil {
			fmt.Fprintln(os.Stderr, "curator wake -now:", err)
			return 2
		}
	}
	card, err := deriveWakeCard(root, path, state, now)
	if err != nil {
		fmt.Fprintln(os.Stderr, "curator wake:", err)
		return 2
	}
	if *asJSON {
		b, _ := json.Marshal(card)
		fmt.Println(string(b))
	} else {
		fmt.Printf("wake dry-run: target=%s ready=%t action=%s reason=%s\n", card.WakeTarget, card.Ready, card.Action, card.Reason)
		if card.ResumeAt != "" {
			fmt.Printf("resume_at: %s\n", card.ResumeAt)
		}
		if card.ResumeCmd != "" {
			fmt.Printf("resume_cmd: %s\n", card.ResumeCmd)
		}
	}
	return 0
}
