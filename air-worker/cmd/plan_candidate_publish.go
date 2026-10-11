package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type planPublishError struct {
	State, Reason string
}

func (e *planPublishError) Error() string { return e.State + ": " + e.Reason }

func candidatePublishDigest(c planCandidate) [32]byte {
	// The private digest seals A1's returned values, including the operation and exact bytes.
	raw, _ := json.Marshal(c)
	return sha256.Sum256(raw)
}

// publishPlanCandidateCAS is fixture-only. Nil means this immutable operation is present,
// including on replay; it never means exclusive claim ownership or LEARN publication.
// shortcut: A1 provenance is process-local, durable recovery belongs to A3.
func publishPlanCandidateCAS(ctx context.Context, repo, originBare, ref string, candidate planCandidate, expectedOldSHA string) error {
	ctx, cancelWork := context.WithTimeout(ctx, 30*time.Second)
	defer cancelWork()
	conflict := func(reason string) error { return &planPublishError{State: "conflict", Reason: reason} }
	pending := func(reason string) error { return &planPublishError{State: "pending", Reason: reason} }
	if ref != "refs/heads/canonical" || !candidateOIDRE.MatchString(expectedOldSHA) ||
		!candidateOIDRE.MatchString(candidate.OID) || !candidateOIDRE.MatchString(candidate.Tree) ||
		candidate.Parent != expectedOldSHA || !candidateOperationRE.MatchString(candidate.Operation) ||
		candidate.preparedDigest != candidatePublishDigest(candidate) {
		return conflict("candidate/ref/operation identity mismatch")
	}
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(key), "GIT_") && value != "" {
			return conflict("unknown Git environment authority: " + key)
		}
	}
	// The receiver inherits these overrides, so bare-repo hooks cannot run either.
	env := append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_NO_REPLACE_OBJECTS=1", "GIT_NO_LAZY_FETCH=1",
		"GIT_CONFIG_COUNT=4", "GIT_CONFIG_KEY_0=core.hooksPath", "GIT_CONFIG_VALUE_0="+os.DevNull,
		"GIT_CONFIG_KEY_1=protocol.allow", "GIT_CONFIG_VALUE_1=never",
		"GIT_CONFIG_KEY_2=protocol.file.allow", "GIT_CONFIG_VALUE_2=always",
		"GIT_CONFIG_KEY_3=core.fsmonitor", "GIT_CONFIG_VALUE_3=false")
	git, err := exec.LookPath("git")
	if err != nil {
		return pending(err.Error())
	}
	run := func(parent context.Context, root string, args ...string) ([]byte, error) {
		bounded, cancel := context.WithTimeout(parent, 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(bounded, git, append([]string{"-C", root}, args...)...)
		cmd.Env, cmd.WaitDelay = env, time.Second
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("git %s: %w: %s", args[0], err, stderr.String())
		}
		return out, nil
	}
	// Both ends must explicitly declare their physical OS-temp identity.
	for _, root := range []string{repo, originBare} {
		if err := candidatePublishFixture(root); err != nil {
			return conflict(err.Error())
		}
	}
	for _, item := range []struct{ root, metadata, bare string }{{repo, filepath.Join(repo, ".git"), "false"}, {originBare, originBare, "true"}} {
		info, err := os.Lstat(item.metadata)
		if err != nil || !info.IsDir() {
			return conflict("standalone Git directory required")
		}
		if err := filepath.WalkDir(item.metadata, func(name string, entry fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				return walkErr
			}
			return candidateNoLinks(name)
		}); err != nil {
			return conflict(err.Error())
		}
		for _, name := range []string{"commondir", "objects/info/alternates", "objects/info/http-alternates", "shallow", "info/grafts", "refs/replace"} {
			if _, err := os.Lstat(filepath.Join(item.metadata, filepath.FromSlash(name))); !os.IsNotExist(err) {
				return conflict("unsupported Git metadata: " + name)
			}
		}
		if item.bare == "true" {
			hooks := filepath.Join(item.metadata, "hooks")
			if err := candidateNoLinks(hooks); err != nil {
				return conflict("unsupported hook path: " + err.Error())
			}
			entries, err := os.ReadDir(hooks)
			if err != nil {
				return conflict("unsupported hook metadata: " + err.Error())
			}
			for _, hook := range entries {
				info, err := hook.Info()
				if err != nil || !info.Mode().IsRegular() || !strings.HasSuffix(hook.Name(), ".sample") || info.Mode().Perm()&0111 != 0 {
					return conflict("active or unsupported receiver hook: " + hook.Name())
				}
			}
		}
		config, err := run(ctx, item.root, "config", "--local", "--no-includes", "--list", "-z")
		if err != nil {
			return conflict(err.Error())
		}
		for _, entry := range bytes.Split(config, []byte{0}) {
			if len(entry) == 0 {
				continue
			}
			key, value, ok := strings.Cut(string(entry), "\n")
			allowed := false
			switch key {
			case "core.repositoryformatversion":
				allowed = value == "0"
			case "core.bare":
				allowed = value == item.bare
			case "core.filemode", "core.logallrefupdates", "core.ignorecase", "core.symlinks":
				allowed = value == "true" || value == "false"
			}
			if !ok || !allowed {
				return conflict("unsupported fixture config: " + key)
			}
		}
		for _, check := range []struct {
			args []string
			want string
		}{
			{[]string{"rev-parse", "--is-bare-repository"}, item.bare},
			{[]string{"rev-parse", "--absolute-git-dir"}, filepath.ToSlash(item.metadata)},
		} {
			out, err := run(ctx, item.root, check.args...)
			if err != nil || strings.TrimSpace(string(out)) != check.want {
				return conflict("Git physical identity mismatch")
			}
		}
	}
	originInfo, err := os.Stat(originBare)
	if err != nil {
		return pending(err.Error())
	}
	verifyOrigin := func() error {
		if err := candidatePublishFixture(originBare); err != nil {
			return err
		}
		now, err := os.Stat(originBare)
		if err != nil {
			return err
		}
		if !os.SameFile(originInfo, now) {
			return fmt.Errorf("origin physical identity changed")
		}
		name := filepath.Join(originBare, "refs", "heads", "canonical")
		if err := candidateNoLinks(name); err != nil {
			return err
		}
		raw, err := os.ReadFile(name)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if bytes.HasPrefix(raw, []byte("ref:")) {
			return fmt.Errorf("symbolic canonical ref forbidden")
		}
		return nil
	}
	meta, err := run(ctx, repo, "show", "-s", "--format=%T%n%P%n%B", candidate.OID)
	want := candidate.Tree + "\n" + expectedOldSHA + "\nAirWorker candidate\n\nOperation-ID: " + candidate.Operation
	if err != nil || strings.TrimSpace(string(meta)) != want {
		return conflict("candidate commit/tree/parent/operation mismatch")
	}
	for name, content := range candidate.Files {
		raw, err := run(ctx, repo, "cat-file", "blob", candidate.OID+":"+name)
		if err != nil || !bytes.Equal(raw, content) {
			return conflict("candidate file mismatch: " + name)
		}
	}
	readback := func(parent context.Context) (string, error) {
		if err := verifyOrigin(); err != nil {
			return "", err
		}
		out, err := run(parent, originBare, "rev-parse", "--verify", ref)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	}
	remote, err := readback(ctx)
	if err != nil {
		return pending(err.Error())
	}
	if remote == candidate.OID {
		return nil
	}
	if remote != expectedOldSHA {
		return conflict("canonical advanced to " + remote)
	}
	if err := verifyOrigin(); err != nil {
		return pending(err.Error())
	}
	_, pushErr := run(ctx, repo, "push", "--porcelain", "--no-verify", "--no-follow-tags", "--recurse-submodules=no", "--signed=false",
		"--force-with-lease="+ref+":"+expectedOldSHA, "--", originBare, candidate.OID+":"+ref)
	// Never replay an unknown transport result: re-read this exact physical origin first.
	recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	remote, err = readback(recovery)
	if err != nil {
		return pending(fmt.Sprintf("readback unavailable: %v; push: %v", err, pushErr))
	}
	if remote == candidate.OID {
		return nil
	}
	if remote != expectedOldSHA {
		return conflict("canonical advanced to " + remote)
	}
	return pending(fmt.Sprintf("candidate not observed; push: %v", pushErr))
}

func candidatePublishFixture(root string) error {
	if !filepath.IsAbs(root) {
		return fmt.Errorf("absolute local fixture path required")
	}
	if err := candidateNoLinks(root); err != nil {
		return err
	}
	temp, err := resolveLinks(os.TempDir())
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(temp, root)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("OS-temp fixture required")
	}
	name := filepath.Join(root, "plan-authority.json")
	if err := candidateNoLinks(name); err != nil {
		return err
	}
	info, err := os.Stat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return fmt.Errorf("fixture authority missing")
	}
	raw, err := os.ReadFile(name)
	if err != nil {
		return err
	}
	var authority struct{ Schema, Mode, Root string }
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&authority) != nil || decoder.Decode(new(any)) != io.EOF || authority.Schema != "air-worker.plan.authority/v1" || authority.Mode != "isolated-fixture" || authority.Root != root {
		return fmt.Errorf("foreign fixture authority")
	}
	return nil
}
