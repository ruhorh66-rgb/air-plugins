package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// removeUnstartedCodingWorker refuses any checkout whose ownership or complete
// worktree state cannot be verified; it never uses force.
func removeUnstartedCodingWorker(repoTop, base, worker, sourceHead string) error {
	repoTop, err := filepath.Abs(repoTop)
	if err != nil {
		return err
	}
	base, err = filepath.Abs(base)
	if err != nil {
		return err
	}
	worker, err = filepath.Abs(worker)
	if err != nil {
		return err
	}
	id := filepath.Base(base)
	if len(id) != 18 {
		return errors.New("invalid reservation identity")
	}
	if _, err := hex.DecodeString(id); err != nil {
		return errors.New("invalid reservation identity")
	}
	expectedBase := filepath.Join(filepath.Dir(repoTop), ".airworker-coding-workspaces", filepath.Base(repoTop), id)
	if !sameCodingPath(base, expectedBase) ||
		(!sameCodingPath(worker, filepath.Join(base, "worker-1")) && !sameCodingPath(worker, filepath.Join(base, "worker-2"))) {
		return errors.New("worker is outside its reserved namespace")
	}
	for _, p := range []string{base, worker} {
		info, err := os.Lstat(p)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("worker path is not an owned real directory")
		}
		real, err := filepath.EvalSymlinks(p)
		if err != nil || !sameCodingPath(real, p) {
			return errors.New("worker path is not canonical")
		}
	}
	top, err := gitCodingOutput(worker, "rev-parse", "--show-toplevel")
	if err != nil || !sameCodingPath(top, worker) {
		return errors.New("worker root is not verified")
	}
	parentCommon, err := gitCodingOutput(repoTop, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	workerCommon, err := gitCodingOutput(worker, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil || !sameCodingPath(parentCommon, workerCommon) {
		return errors.New("worker belongs to a different repository")
	}
	head, err := gitCodingOutput(worker, "rev-parse", "HEAD")
	if err != nil || head != sourceHead {
		return errors.New("worker base commit is not verified")
	}
	status, err := exec.Command("git", "-C", worker, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignored").Output()
	if err != nil || len(status) != 0 {
		return errors.New("worker has unverified changes or ignored data")
	}
	if _, err := gitCodingOutput(repoTop, "worktree", "remove", worker); err != nil {
		return fmt.Errorf("owned unstarted worktree retained: %w", err)
	}
	if _, err := os.Lstat(worker); !os.IsNotExist(err) {
		return errors.New("worktree removal not confirmed")
	}
	return nil
}
