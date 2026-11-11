package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Only worker-owned .woody files may accompany the declared source change.
// Other untracked data holds the checkout for a human decision.
func codingArchiveExtraPaths(workerTop, productRel string) ([]string, error) {
	dirs, err := exec.Command("git", "-C", workerTop, "ls-files", "--others", "--directory", "--full-name", "-z", "--", ".").Output()
	if err != nil {
		return nil, err
	}
	if len(dirs) > 0 && dirs[len(dirs)-1] != 0 {
		return nil, errors.New("unterminated worker directory list")
	}
	prefix := filepath.ToSlash(filepath.Join(productRel, ".woody")) + "/"
	for _, name := range strings.Split(strings.TrimSuffix(string(dirs), "\x00"), "\x00") {
		if name != "" && strings.HasSuffix(name, "/") && !strings.HasPrefix(name, prefix) {
			return nil, errors.New("worker has an unverified untracked directory")
		}
	}
	woody := filepath.Join(workerTop, productRel, ".woody")
	if err := filepath.WalkDir(woody, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("worker .woody contains a link")
		}
		if entry.IsDir() {
			children, err := os.ReadDir(path)
			if err != nil {
				return err
			}
			if len(children) == 0 {
				return errors.New("worker .woody contains an unverified empty directory")
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
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
