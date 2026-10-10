package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A stage label such as "Этап 1" remains supported for historical
// plans. N-* parent pointers, unlike stage labels, are machine identities:
// they must resolve to EXACT canonical node IDs, never a near match.
func validateExistingPlanParent(root, parent, childID string) error {
	parent = strings.TrimSpace(parent)
	if parent == "" {
		return errors.New("plan node parent is required")
	}
	if parent == "PLAN.md" {
		if _, err := os.Stat(filepath.Join(root, parent)); err != nil {
			return fmt.Errorf("plan root unavailable: %w", err)
		}
		return nil
	}
	if !strings.HasPrefix(parent, "N-") {
		return nil // Historical named stage; retain compatibility.
	}
	seen := map[string]bool{}
	for strings.HasPrefix(parent, "N-") {
		if parent == childID {
			return fmt.Errorf("plan node parent would create a cycle at %s", childID)
		}
		if seen[parent] {
			return fmt.Errorf("existing plan node ancestry contains a cycle at %s", parent)
		}
		seen[parent] = true
		if len(seen) > 256 {
			return errors.New("plan node parent hierarchy exceeds 256 levels")
		}
		path, err := planNodePathByID(root, parent)
		if err != nil {
			return fmt.Errorf("plan node parent %q does not exist: %w", parent, err)
		}
		n, err := readPlanNode(path)
		if err != nil {
			return fmt.Errorf("invalid ancestor %s: %w", parent, err)
		}
		if n.ID != parent {
			return fmt.Errorf("plan node parent %q is not the exact canonical identity %q", parent, n.ID)
		}
		parent = strings.TrimSpace(n.Parent)
	}
	return nil
}
