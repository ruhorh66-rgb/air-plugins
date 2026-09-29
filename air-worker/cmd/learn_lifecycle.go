package main

import (
	"fmt"
	"strings"
	"time"
)

func recordLearnLifecycleEvent(product, action string, proposal learnProposal, ledgerID, detail string, now time.Time) error {
	action = strings.ToLower(strings.TrimSpace(action))
	if action != "apply" && action != "rollback" {
		return fmt.Errorf("unsupported learn lifecycle action %q", action)
	}
	id, err := newLearnID("LR", now)
	if err != nil {
		return err
	}
	status := learnApplied
	if action == "rollback" {
		status = learnRevoked
	}
	evidence := fmt.Sprintf("action=%s ledger=%s", action, strings.TrimSpace(ledgerID))
	if strings.TrimSpace(detail) != "" {
		evidence += " " + strings.TrimSpace(detail)
	}
	row := learnJournalRecord{
		Schema: learnSchemaVersion, ID: id, CreatedAt: now.UTC().Format(time.RFC3339Nano),
		Class: proposal.Class, Observed: fmt.Sprintf("proposal %s lifecycle -> %s", proposal.ID, status),
		Evidence: evidence, Kind: "lifecycle", Source: "air-worker-learn",
		Actor: "core", Reference: proposal.ID,
	}
	if err := validateLearnEvent(row); err != nil {
		return err
	}
	return appendLearnJSON(learnPaths(product).Journal, row)
}
