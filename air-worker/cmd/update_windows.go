//go:build windows

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func launchAirWorkerUpdateApply(m updateManifest, stage, manifestPath, home string) error {
	stagedCLI := filepath.Join(stage, "air-worker.exe")
	if err := verifyDownloadedArtifact(stagedCLI, m.CLI); err != nil {
		return err
	}
	args := []string{
		"update", "_apply",
		"-manifest", manifestPath,
		"-stage", stage,
		"-home", home,
		"-parent", strconv.Itoa(os.Getpid()),
	}
	const (
		createNewProcessGroup  = 0x00000200
		detachedProcess        = 0x00000008
		createBreakawayFromJob = 0x01000000
	)
	start := func(flags uint32) error {
		cmd := exec.Command(stagedCLI, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: flags}
		if err := cmd.Start(); err != nil {
			return err
		}
		return cmd.Process.Release()
	}
	if err := start(createNewProcessGroup | detachedProcess | createBreakawayFromJob); err == nil {
		return nil
	}
	return start(createNewProcessGroup | detachedProcess)
}

func waitUpdateParentExit(pid int, timeout time.Duration) error {
	if pid <= 0 {
		return nil
	}
	const (
		synchronize = 0x00100000
		waitObject0 = 0
		waitTimeout = 258
	)
	h, _, callErr := adapterOpenProcess.Call(synchronize, 0, uintptr(uint32(pid)))
	if h == 0 {
		if errno, ok := callErr.(syscall.Errno); ok && uintptr(errno) == 87 {
			return nil // ERROR_INVALID_PARAMETER: parent already gone.
		}
		return fmt.Errorf("open update parent %d: %v", pid, callErr)
	}
	defer adapterCloseHandle.Call(h)
	ms := uint32(timeout / time.Millisecond)
	result, _, _ := adapterWaitForSingleObject.Call(h, uintptr(ms))
	switch result {
	case waitObject0:
		return nil
	case waitTimeout:
		return fmt.Errorf("old air-worker process %d did not exit within %s", pid, timeout)
	default:
		return fmt.Errorf("wait old air-worker process %d returned %d", pid, result)
	}
}

func copyUpdateSnapshot(src, dst string) error {
	st, err := os.Stat(src)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if st.IsDir() {
		return nil
	}
	return copyFile(src, dst)
}

func restoreUpdateSnapshot(snapshotDir, home string) error {
	binDir := filepath.Join(home, "bin")
	var problems []string
	for _, name := range []string{"air-worker.exe", "air-worker-tray.exe"} {
		src := filepath.Join(snapshotDir, name)
		if _, err := os.Stat(src); err != nil {
			if !os.IsNotExist(err) {
				problems = append(problems, err.Error())
			}
			continue
		}
		if err := copyFile(src, filepath.Join(binDir, name)); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", name, err))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("rollback restore: %s", strings.Join(problems, "; "))
	}
	return nil
}

func writeUpdateReceipt(name string, value any) {
	path := filepath.Join(updateRootDir(), name)
	_ = writeUpdateJSON(path, value)
}

func runUpdateSelfcheck(livePath string) error {
	ctxTimeout := 45 * time.Second
	cmd := exec.Command(livePath, "selfcheck", "-json", "-live", livePath)
	done := make(chan error, 1)
	var out []byte
	go func() {
		var err error
		out, err = cmd.CombinedOutput()
		done <- err
	}()
	select {
	case err := <-done:
		if len(out) > 0 {
			_ = os.WriteFile(filepath.Join(updateRootDir(), "selfcheck-after.json"), out, 0o600)
		}
		if err != nil {
			return fmt.Errorf("post-update selfcheck failed: %w", err)
		}
		return nil
	case <-time.After(ctxTimeout):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return fmt.Errorf("post-update selfcheck timed out after %s", ctxTimeout)
	}
}

func replaceUpdatePairTransactional(
	snapshotDir, home, stagedCLI, stagedTray string,
	m updateManifest,
	accept func(liveCLI string) error,
) error {
	binDir := filepath.Join(home, "bin")
	dstCLI := filepath.Join(binDir, "air-worker.exe")
	dstTray := filepath.Join(binDir, "air-worker-tray.exe")
	fail := func(cause error) error {
		if restoreErr := restoreUpdateSnapshot(snapshotDir, home); restoreErr != nil {
			return fmt.Errorf("%v; %w", cause, restoreErr)
		}
		return cause
	}

	if err := copyFile(stagedCLI, dstCLI); err != nil {
		return fail(fmt.Errorf("replace CLI: %w", err))
	}
	if err := copyFile(stagedTray, dstTray); err != nil {
		return fail(fmt.Errorf("replace tray: %w", err))
	}
	if err := verifyDownloadedArtifact(dstCLI, m.CLI); err != nil {
		return fail(fmt.Errorf("verify installed CLI: %w", err))
	}
	if err := verifyDownloadedArtifact(dstTray, m.Tray); err != nil {
		return fail(fmt.Errorf("verify installed tray: %w", err))
	}
	if accept != nil {
		if err := accept(dstCLI); err != nil {
			return fail(err)
		}
	}
	return nil
}

func cmdUpdateApply(argv []string) int {
	fs := flag.NewFlagSet("update _apply", flag.ContinueOnError)
	manifestPath := fs.String("manifest", "", "signed manifest path")
	stage := fs.String("stage", "", "verified staging directory")
	home := fs.String("home", "", "installed AirWorker home")
	parent := fs.Int("parent", 0, "old updater pid")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if strings.TrimSpace(*manifestPath) == "" || strings.TrimSpace(*stage) == "" || strings.TrimSpace(*home) == "" {
		fmt.Fprintln(os.Stderr, "update _apply requires -manifest, -stage and -home")
		return 2
	}

	raw, err := os.ReadFile(*manifestPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var m updateManifest
	if err := json.Unmarshal(stripUTF8BOM(raw), &m); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	cfg, err := loadUpdateConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if m.Version != version {
		fmt.Fprintf(os.Stderr, "staged binary version %s != manifest %s%s", version, m.Version, lineEnding)
		return 2
	}
	if _, err := verifyUpdateManifest(m, m.Version, cfg.Channel); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	stagedCLI := filepath.Join(*stage, "air-worker.exe")
	stagedTray := filepath.Join(*stage, "air-worker-tray.exe")
	if err := verifyDownloadedArtifact(stagedCLI, m.CLI); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := verifyDownloadedArtifact(stagedTray, m.Tray); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if got := binaryVersion(stagedCLI); got != m.Version {
		fmt.Fprintf(os.Stderr, "staged CLI version=%s want=%s%s", got, m.Version, lineEnding)
		return 2
	}

	if err := waitUpdateParentExit(*parent, 60*time.Second); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	lock, ok := acquireLock(`Local\air-worker-install`)
	if !ok {
		fmt.Fprintln(os.Stderr, "another AirWorker install/update is running")
		return 1
	}
	defer lock.release()

	if asked, gone := stopTray(); asked && !gone {
		fmt.Fprintln(os.Stderr, "tray did not stop; update aborted before replacing files")
		return 1
	}

	binDir := filepath.Join(*home, "bin")
	dstCLI := filepath.Join(binDir, "air-worker.exe")
	dstTray := filepath.Join(binDir, "air-worker-tray.exe")
	if _, err := os.Stat(dstCLI); err != nil {
		fmt.Fprintf(os.Stderr, "live CLI missing: %v%s", err, lineEnding)
		return 2
	}
	if _, err := os.Stat(dstTray); err != nil {
		fmt.Fprintf(os.Stderr, "live tray missing: %v%s", err, lineEnding)
		return 2
	}
	oldVersion := binaryVersion(dstCLI)
	if oldVersion == "" {
		fmt.Fprintln(os.Stderr, "live CLI did not report its version; update aborted before replacement")
		return 2
	}

	snapshotDir := filepath.Join(updateRootDir(), "rollback", time.Now().UTC().Format("20060102T150405.000000000Z"))
	if err := os.MkdirAll(snapshotDir, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	for _, pair := range [][2]string{
		{dstCLI, filepath.Join(snapshotDir, "air-worker.exe")},
		{dstTray, filepath.Join(snapshotDir, "air-worker-tray.exe")},
		{updateConfigPath(), filepath.Join(snapshotDir, "update.json")},
		{updateStatePath(), filepath.Join(snapshotDir, "update-state.json")},
	} {
		if err := copyUpdateSnapshot(pair[0], pair[1]); err != nil {
			fmt.Fprintf(os.Stderr, "snapshot failed: %v%s", err, lineEnding)
			return 2
		}
	}

	rollback := func(cause error) int {
		restoreErr := restoreUpdateSnapshot(snapshotDir, *home)
		st := updateState{
			CurrentVersion: oldVersion,
			Channel:        cfg.Channel,
			LatestVersion:  m.Version,
			LatestRevision: m.VCSRevision,
			PayloadSHA256:  m.PayloadSHA256,
			Phase:          "rolled_back",
			StageDir:       *stage,
			Error:          cause.Error(),
		}
		if restoreErr != nil {
			st.Error += "; " + restoreErr.Error()
		}
		_ = writeUpdateState(st)
		writeUpdateReceipt("update-result.json", st)
		if !trayDisabled() {
			_ = startTrayDetached(dstTray)
		}
		fmt.Fprintln(os.Stderr, st.Error)
		if restoreErr != nil {
			return 2
		}
		return 1
	}

	if err := replaceUpdatePairTransactional(
		snapshotDir, *home, stagedCLI, stagedTray, m,
		func(liveCLI string) error {
			if err := verifyUpdateLiveIdentity(m, liveCLI); err != nil {
				return fmt.Errorf("distribution identity: %w", err)
			}
			return runUpdateSelfcheck(liveCLI)
		},
	); err != nil {
		return rollback(err)
	}

	if !trayDisabled() {
		if err := startTrayDetached(dstTray); err != nil {
			return rollback(fmt.Errorf("restart tray: %w", err))
		}
	}
	st := updateState{
		CurrentVersion:  m.Version,
		Channel:         cfg.Channel,
		UpdateAvailable: false,
		LatestVersion:   m.Version,
		LatestRevision:  m.VCSRevision,
		PayloadSHA256:   m.PayloadSHA256,
		Phase:           "installed",
		StageDir:        *stage,
	}
	if err := writeUpdateState(st); err != nil {
		return rollback(fmt.Errorf("write installed update state: %w", err))
	}
	writeUpdateReceipt("update-result.json", st)
	return 0
}
