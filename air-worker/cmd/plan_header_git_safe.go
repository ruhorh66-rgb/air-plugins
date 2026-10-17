package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// A Git runner is pinned to exact build-time tool identity. There is no
// fallback to PATH, local repo config, or unverified runtime overrides.
// Source builds without these pins are diagnostic BLOCKED by design.
var (
	planHeaderTrustedGitPath   string
	planHeaderTrustedGitSHA256 string
)

type planHeaderGitTool struct {
	Path   string
	SHA256 string
}

func planHeaderConfiguredGitTool() planHeaderGitTool {
	return planHeaderGitTool{Path: planHeaderTrustedGitPath, SHA256: planHeaderTrustedGitSHA256}
}

func planHeaderVerifyGitPin(tool planHeaderGitTool) error {
	if !filepath.IsAbs(tool.Path) || filepath.Clean(tool.Path) != tool.Path ||
		!planHeaderFileSHARegex.MatchString(tool.SHA256) {
		return errors.New("trusted absolute Git path and exact SHA not configured")
	}
	if err := planHeaderValidateAncestors(filepath.Dir(tool.Path)); err != nil {
		return fmt.Errorf("Git tool ancestor not trusted: %w", err)
	}
	real, err := filepath.EvalSymlinks(tool.Path)
	if err != nil || !strings.EqualFold(filepath.Clean(real), tool.Path) {
		return errors.New("Git binary path is reparse/redirected")
	}
	st, err := os.Lstat(tool.Path)
	if err != nil || !st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0 || st.Size() > 300*1024*1024 {
		return errors.New("Git tool is not a trusted regular file")
	}
	f, err := os.Open(tool.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(opened, st) {
		return errors.New("Git binary changed while opening")
	}
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, 300*1024*1024+1)); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != tool.SHA256 {
		return errors.New("Git binary SHA256 differs from compiled-in pin")
	}
	return nil
}

func planHeaderGitRead(root string, args ...string) (string, error) {
	return planHeaderGitReadPinned(planHeaderConfiguredGitTool(), root, args...)
}

func planHeaderGitReadPinned(tool planHeaderGitTool, root string, args ...string) (string, error) {
	return planHeaderExecutePinnedGit(tool, root, args...)
}

func planHeaderExecutePinnedGit(tool planHeaderGitTool, directory string, args ...string) (string, error) {
	if err := planHeaderVerifyGitPin(tool); err != nil {
		return "", err
	}
	if err := planHeaderValidateAncestors(directory); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	// Global/system executable helpers, url rewrites, prompts and transport
	// hooks are disabled. Repository-local configuration may be read for
	// source metadata; remote network calls never run in that repository.
	guard := []string{
		"-c", "core.fsmonitor=false",
		"-c", "core.hooksPath=" + os.DevNull,
		"-c", "credential.helper=",
		"-c", "credential.interactive=never",
		"-c", "http.followRedirects=false",
		"-c", "protocol.allow=never",
		"-c", "protocol.https.allow=always",
		"-c", "protocol.ext.allow=never",
		"-c", "protocol.file.allow=never",
		"-c", "protocol.ssh.allow=never",
		"-c", "protocol.git.allow=never",
		"--no-optional-locks",
	}
	cmd := exec.CommandContext(ctx, tool.Path, append(guard, args...)...)
	cmd.Env = planHeaderSafeGitEnvironment()
	cmd.Dir = directory
	out, err := cmd.CombinedOutput()
	// Even if the process was replaced in a race, it cannot authorize an
	// accepted proof without passing this post-launch pin check.
	if pinErr := planHeaderVerifyGitPin(tool); pinErr != nil {
		return "", pinErr
	}
	if err != nil || len(out) > 65536 {
		return "", errors.New("pinned Git preflight unavailable")
	}
	s := strings.TrimSpace(string(out))
	if !utf8.ValidString(s) {
		return "", errors.New("Git output is not valid UTF-8")
	}
	return s, nil
}

func planHeaderValidateRepository(root string, tool planHeaderGitTool) error {
	if !filepath.IsAbs(root) {
		return errors.New("product root is not absolute")
	}
	if err := planHeaderValidateAncestors(root); err != nil {
		return err
	}
	// Check both the discovered worktree top-level and Git's relative prefix.
	top, err := planHeaderGitReadPinned(tool, root, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("Git top level unavailable: %w", err)
	}
	prefix, err := planHeaderGitReadPinned(tool, root, "rev-parse", "--show-prefix")
	if err != nil {
		return fmt.Errorf("Git product prefix unavailable: %w", err)
	}
	if !filepath.IsAbs(top) {
		return errors.New("Git reported relative top level")
	}
	resolvedTop, err := filepath.EvalSymlinks(top)
	if err != nil || !strings.EqualFold(filepath.Clean(resolvedTop), filepath.Clean(top)) {
		return errors.New("Git top level is a reparse/redirected directory")
	}
	if err := planHeaderValidateAncestors(top); err != nil {
		return err
	}
	expectedRoot := filepath.Clean(top)
	if strings.EqualFold(filepath.Clean(root), expectedRoot) && prefix == "" {
		return nil
	}
	// Known dual layout: private monorepo root with single air-worker product.
	if prefix == "air-worker/" &&
		strings.EqualFold(filepath.Clean(root), filepath.Join(expectedRoot, "air-worker")) {
		return nil
	}
	return errors.New("Git repository top level or product prefix does not match expected root")
}

func planHeaderRemoteDirectory(tool planHeaderGitTool) (string, error) {
	drive := filepath.VolumeName(tool.Path)
	if drive == "" {
		return "", errors.New("Git binary lacks absolute volume")
	}
	dir := drive + string(os.PathSeparator)
	if err := planHeaderValidateAncestors(dir); err != nil {
		return "", err
	}
	if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
		return "", errors.New("trusted isolated Git directory unexpectedly contains repository configuration")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return dir, nil
}

func planHeaderGitRemoteRead(tool planHeaderGitTool, remoteURL, ref string) (string, error) {
	if !planHeaderAllowedRemote(remoteURL) {
		return "", errors.New("untrusted Git remote URL")
	}
	if !strings.HasPrefix(ref, "refs/heads/") || !planHeaderBranchRegex.MatchString(strings.TrimPrefix(ref, "refs/heads/")) {
		return "", errors.New("unsafe Git remote branch ref")
	}
	dir, err := planHeaderRemoteDirectory(tool)
	if err != nil {
		return "", err
	}
	// Unlike "git ls-remote <remote-name>", this detached command ignores
	// remote.<name>.vcs and local url.*.insteadOf from the candidate repo.
	// The exact validated HTTPS URL is the only network target.
	return planHeaderExecutePinnedGit(tool, dir, "ls-remote", "--exit-code", remoteURL, ref)
}

// Read and pin the PLAN file itself, not just its parent. All returned facts
// are preview-only until the native N124 generation lock is available.
func planHeaderReadPlan(root string) ([]byte, error) {
	if err := planHeaderValidateAncestors(root); err != nil {
		return nil, err
	}
	path := filepath.Join(root, "PLAN.md")
	st, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0 || st.Size() > 8*1024*1024 {
		return nil, errors.New("PLAN leaf is linked, irregular or too large")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || !strings.EqualFold(filepath.Clean(resolved), filepath.Clean(path)) {
		return nil, errors.New("PLAN leaf is reparse/redirected")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	openInfo, err := f.Stat()
	if err != nil || !os.SameFile(st, openInfo) {
		return nil, errors.New("PLAN leaf identity changed")
	}
	body, err := io.ReadAll(io.LimitReader(f, 8*1024*1024+1))
	if err != nil || len(body) > 8*1024*1024 {
		return nil, errors.New("PLAN read failure or oversized")
	}
	latest, err := os.Lstat(path)
	if err != nil || !os.SameFile(st, latest) {
		return nil, errors.New("PLAN leaf changed during read")
	}
	return body, nil
}
