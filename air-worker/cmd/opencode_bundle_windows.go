//go:build windows

package main

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	openCodeBundleVersion       = "1.18.31"
	openCodeBundleArchive       = "opencode-windows-x64-baseline-v1.18.31.zip"
	openCodeBundleArchiveSHA256 = "7c4fc9be7124df5e7c42184b99e8d8540fb0863bb0378b0c4219d9567b2d8434"
	openCodeBundleEXESHA256     = "a6167edb2f47fee14e834b8566fefadaa85ef031b8993ab3fd860899281a2856"
)

type routerRuntimeStage struct {
	dir      string
	bridge   string
	opencode string
}

func payloadFile(srcDir string, parts ...string) (string, error) {
	roots := []string{filepath.Dir(srcDir), srcDir}
	var tried []string
	for _, root := range roots {
		p := filepath.Join(append([]string{root}, parts...)...)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
		tried = append(tried, p)
	}
	return "", fmt.Errorf("release payload Ð½Ðµ Ð½Ð°Ð¹Ð´ÐµÐ½: %s", strings.Join(tried, "; "))
}

func verifyOpenCodeBinary(path string) error {
	h, err := sha256File(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(h, openCodeBundleEXESHA256) {
		return fmt.Errorf("OpenCode SHA-256 %s, Ð¾Ð¶Ð¸Ð´Ð°Ð»ÑÑ %s", h, openCodeBundleEXESHA256)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("opencode --version: %w", err)
	}
	if got := strings.TrimSpace(string(out)); got != openCodeBundleVersion {
		return fmt.Errorf("OpenCode version %q, Ð¾Ð¶Ð¸Ð´Ð°Ð»Ð°ÑÑŒ %q", got, openCodeBundleVersion)
	}
	return nil
}
func extractOpenCodeArchive(archivePath, dst string) error {
	h, err := sha256File(archivePath)
	if err != nil {
		return err
	}
	if !strings.EqualFold(h, openCodeBundleArchiveSHA256) {
		return fmt.Errorf("OpenCode archive SHA-256 %s, Ð¾Ð¶Ð¸Ð´Ð°Ð»ÑÑ %s", h, openCodeBundleArchiveSHA256)
	}
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if filepath.ToSlash(f.Name) != "opencode.exe" || f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		out, err := os.Create(dst)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, rc)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	return fmt.Errorf("Ð² OpenCode archive Ð½ÐµÑ‚ opencode.exe")
}
func stageRouterRuntimePayload(srcDir string) (*routerRuntimeStage, error) {
	tmp, err := os.MkdirTemp("", "air-worker-router-runtime-")
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*routerRuntimeStage, error) {
		_ = os.RemoveAll(tmp)
		return nil, err
	}
	bridgeSrc, err := payloadFile(srcDir, "tools", "router_stream_bridge.py")
	if err != nil {
		return fail(err)
	}
	stage := &routerRuntimeStage{
		dir:      tmp,
		bridge:   filepath.Join(tmp, "router_stream_bridge.py"),
		opencode: filepath.Join(tmp, "opencode.exe"),
	}
	if err := copyFile(bridgeSrc, stage.bridge); err != nil {
		return fail(err)
	}
	archive, archiveErr := payloadFile(srcDir, "vendor", "opencode", openCodeBundleArchive)
	if archiveErr == nil {
		if err := extractOpenCodeArchive(archive, stage.opencode); err != nil {
			return fail(err)
		}
	} else {
		existing, err := payloadFile(srcDir, "tools", "opencode", "opencode.exe")
		if err != nil {
			return fail(archiveErr)
		}
		if err := copyFile(existing, stage.opencode); err != nil {
			return fail(err)
		}
	}
	if err := verifyOpenCodeBinary(stage.opencode); err != nil {
		return fail(err)
	}
	return stage, nil
}
func (s *routerRuntimeStage) cleanup() {
	if s != nil && s.dir != "" {
		_ = os.RemoveAll(s.dir)
	}
}

func installRouterRuntimePayload(stage *routerRuntimeStage, home string) error {
	if stage == nil {
		return fmt.Errorf("router runtime stage Ð¾Ñ‚ÑÑƒÑ‚ÑÑ‚Ð²ÑƒÐµÑ‚")
	}
	dstBridge := filepath.Join(home, "tools", "router_stream_bridge.py")
	dstOpenCode := filepath.Join(home, "tools", "opencode", "opencode.exe")
	if err := copyFile(stage.bridge, dstBridge); err != nil {
		return fmt.Errorf("bridge: %w", err)
	}
	if err := copyFile(stage.opencode, dstOpenCode); err != nil {
		return fmt.Errorf("OpenCode: %w", err)
	}
	if err := verifyOpenCodeBinary(dstOpenCode); err != nil {
		return fmt.Errorf("installed OpenCode: %w", err)
	}
	return nil
}
