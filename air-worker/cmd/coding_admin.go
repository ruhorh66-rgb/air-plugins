package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Worktree-specific Git metadata disappears on removal; reject state that is
// not reconstructible from the archived base commit and verified patch.
func verifyCodingAdminDisposable(workerTop, sourceHead string) error {
	admin, err := gitCodingOutput(workerTop, "rev-parse", "--path-format=absolute", "--git-dir")
	if err != nil {
		return err
	}
	common, err := gitCodingOutput(workerTop, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	admin, err = filepath.Abs(admin)
	if err != nil {
		return err
	}
	common, err = filepath.Abs(common)
	if err != nil || !sameCodingPath(filepath.Dir(admin), filepath.Join(common, "worktrees")) {
		return errors.New("worker Git metadata is outside the common worktree registry")
	}
	info, err := os.Lstat(admin)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("worker Git metadata directory is not verified")
	}
	resolved, err := filepath.EvalSymlinks(admin)
	if err != nil || !sameCodingPath(resolved, admin) {
		return errors.New("worker Git metadata path is not canonical")
	}
	entries, err := os.ReadDir(admin)
	if err != nil {
		return err
	}
	allowed := map[string]bool{"HEAD": true, "ORIG_HEAD": true, "commondir": true, "gitdir": true, "index": true, "logs": true, "refs": true}
	for _, entry := range entries {
		name := entry.Name()
		if !allowed[name] || entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("worker Git metadata retained: %s", name)
		}
		if name == "logs" || name == "refs" {
			if !entry.IsDir() {
				return errors.New("worker Git metadata directory is invalid")
			}
		} else if !entry.Type().IsRegular() {
			return errors.New("worker Git metadata file is invalid")
		}
		switch name {
		case "HEAD", "ORIG_HEAD":
			raw, err := readOwnedCodingArchiveInput(admin, filepath.Join(admin, name), 1024)
			if err != nil || strings.TrimSpace(string(raw)) != sourceHead {
				return errors.New("worker Git HEAD history is not reconstructible")
			}
		case "refs":
			refs, err := os.ReadDir(filepath.Join(admin, name))
			if err != nil || len(refs) != 0 {
				return errors.New("worker has private Git refs")
			}
		case "logs":
			logs, err := os.ReadDir(filepath.Join(admin, name))
			if err != nil {
				return err
			}
			for _, log := range logs {
				if log.Name() != "HEAD" || !log.Type().IsRegular() {
					return errors.New("worker has unverified Git logs")
				}
				raw, err := readOwnedCodingArchiveInput(admin, filepath.Join(admin, name, "HEAD"), 1<<20)
				if err != nil {
					return err
				}
				for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
					fields := strings.Fields(line)
					if len(fields) < 2 {
						return errors.New("worker Git reflog is invalid")
					}
					for _, sha := range fields[:2] {
						if sha != sourceHead && sha != strings.Repeat("0", len(sourceHead)) {
							return errors.New("worker Git reflog contains another revision")
						}
					}
				}
			}
		}
	}
	tracked, err := exec.Command("git", "-C", workerTop, "ls-files", "-v", "-z").Output()
	if err != nil {
		return err
	}
	if len(tracked) != 0 && tracked[len(tracked)-1] != 0 {
		return errors.New("unterminated Git index listing")
	}
	for _, entry := range strings.Split(strings.TrimSuffix(string(tracked), "\x00"), "\x00") {
		if entry != "" && !strings.HasPrefix(entry, "H ") {
			return errors.New("worker index contains hidden or unverified entries")
		}
	}
	return nil
}
