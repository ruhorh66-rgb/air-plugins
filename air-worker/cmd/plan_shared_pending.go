package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type sharedPlanIntentHeader struct {
	Schema      string `json:"schema"`
	Product     string `json:"product"`
	ProductID   string `json:"product_id"`
	RuntimeRoot string `json:"runtime_root"`
	Phase       string `json:"phase"`
}

// The caller already owns the product plan-nodes mutex. A prepared transaction
// reserves both its plan snapshot and any node IDs it selected. Until that
// transaction is retried/reconciled, a different mutation must not allocate IDs,
// publish nodes, alter PLAN.md, or emit learning events.
func rejectConflictingSharedPlanIntent(root string, s sharedLearningSettings, allowedPath string) error {
	allowedPath = filepath.Clean(allowedPath)
	for _, dirName := range []string{"plan-create", "plan-close"} {
		dir := filepath.Join(s.RuntimeRoot, dirName)
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect pending plan transactions: %w", err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			if filepath.Clean(path) == allowedPath {
				continue
			}
			b, err := readLearningBounded(path, 8*sharedLearningMaxBytes)
			if err != nil {
				return fmt.Errorf("cannot inspect pending plan transaction %s: %w", path, err)
			}
			var h sharedPlanIntentHeader
			if err := json.Unmarshal(b, &h); err != nil {
				return fmt.Errorf("cannot decode pending plan transaction %s: %w", path, err)
			}
			switch h.Phase {
			case "complete":
				continue
			case "prepared":
				if filepath.Clean(h.Product) != filepath.Clean(root) ||
					h.ProductID != s.ProductID ||
					filepath.Clean(h.RuntimeRoot) != filepath.Clean(s.RuntimeRoot) {
					return fmt.Errorf("pending plan transaction identity mismatch: %s", path)
				}
				return fmt.Errorf("pending plan transaction requires reconciliation before another mutation: %s", path)
			default:
				return fmt.Errorf("plan transaction has unknown phase %q: %s", h.Phase, path)
			}
		}
	}
	return nil
}
