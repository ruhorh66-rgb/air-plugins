package main

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Only worker-owned .woody files may accompany the declared source change.
// Other untracked data holds the checkout for a human decision.
func codingArchiveExtraPaths(workerTop, productRel string) ([]string, error) {
	raw, err := exec.Command("git", "-C", workerTop, "ls-files", "--others", "--full-name", "-z", "--", ".").Output()
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	if raw[len(raw)-1] != 0 {
		return nil, errors.New("unterminated worker extra file list")
	}
	prefix := filepath.ToSlash(filepath.Join(productRel, ".woody")) + "/"
	task := prefix + "TASK-CODE-WORKER.md"
	var paths []string
	for _, name := range strings.Split(string(raw[:len(raw)-1]), "\x00") {
		if !filepath.IsLocal(filepath.FromSlash(name)) || !strings.HasPrefix(name, prefix) {
			return nil, errors.New("worker has unverified data outside its owned .woody directory")
		}
		if name != task {
			paths = append(paths, name)
		}
	}
	if len(paths) > 2048 {
		return nil, errors.New("worker has too many extra files to archive safely")
	}
	sort.Strings(paths)
	for i := 1; i < len(paths); i++ {
		if paths[i] == paths[i-1] {
			return nil, errors.New("duplicate worker extra file")
		}
	}
	return paths, nil
}

func codingArchiveExtraName(worker int, path string) string {
	return fmt.Sprintf("worker-%d.extra-%s", worker, shaCodingBytes([]byte(path)))
}
