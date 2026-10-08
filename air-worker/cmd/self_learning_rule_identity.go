package main

import (
	"os"
	"path/filepath"
	"strings"
)

// currentSelfLearningRuleBytes verifies that a delivered learned procedure still
// has the exact bytes in the current host/run context. A historical load receipt
// is not proof that the on-disk rule is still the one which was applied.
func currentSelfLearningRuleBytes(owner selfLearningOwner, rule selfLearningLoadedRule) bool {
	root := strings.TrimSpace(owner.Selector.ProductRoot)
	prefix := strings.TrimSuffix(filepath.ToSlash(strings.TrimSpace(owner.Settings.ManagedSkillPrefix)), "/")
	target := strings.TrimSpace(rule.Target)
	if root == "" || prefix == "" || !filepath.IsLocal(filepath.FromSlash(prefix)) ||
		target == "" || strings.Contains(target, "\\") ||
		!strings.HasPrefix(target, prefix+"/") || len(rule.SHA256) != 64 {
		return false
	}
	rel := filepath.FromSlash(target)
	if !filepath.IsLocal(rel) || filepath.ToSlash(filepath.Clean(rel)) != target {
		return false
	}
	// A junction, symlink or reparse directory may escape GitRoot despite a
	// locally clean-looking relative path. Never use its contents as proof.
	current := root
	parts := strings.Split(target, "/")
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		if _, err := os.Readlink(current); err == nil {
			return false
		}
		if i == len(parts)-1 {
			if !info.Mode().IsRegular() || info.Size() > 64*1024 {
				return false
			}
		} else if !info.IsDir() {
			return false
		}
	}
	data, err := readLearningBounded(current, 64*1024)
	if err != nil {
		return false
	}
	return strings.EqualFold(learnSHA(data), rule.SHA256)
}
