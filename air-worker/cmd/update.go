package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	updateSchema = 1
	updateKeyID  = "air-worker-update-2026-01"

	// Закрытый ключ хранится вне Git. В клиенте только ключ проверки.
	updatePublicKeyB64 = "caOGBxSFSdnODzMsPtikaZKrc6qN/v6pGf8VOms2cUE="

	updateStableManifestURL     = "https://raw.githubusercontent.com/ruhorh66-rgb/air-plugins/air-worker-update-feed/updates/air-worker/stable/update-manifest.json"
	updatePrereleaseManifestURL = "https://raw.githubusercontent.com/ruhorh66-rgb/air-plugins/air-worker-update-feed/updates/air-worker/prerelease/update-manifest.json"

	maxUpdateManifestBytes = 1 << 20
)

type updateConfig struct {
	Schema             int    `json:"schema"`
	Channel            string `json:"channel"`
	CheckIntervalHours int    `json:"check_interval_hours"`
}

type updateArtifact struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type updateManifest struct {
	Schema          int            `json:"schema"`
	Channel         string         `json:"channel"`
	Version         string         `json:"version"`
	PublishedAt     string         `json:"published_at"`
	VCSRevision     string         `json:"vcs_revision"`
	PayloadSHA256   string         `json:"payload_sha256"`
	CLI             updateArtifact `json:"cli"`
	Tray            updateArtifact `json:"tray"`
	SigningKeyID    string         `json:"signing_key_id"`
	ManifestSig     string         `json:"manifest_signature"`
	ReleaseNotesURL string         `json:"release_notes_url,omitempty"`
}

type updateState struct {
	Schema          int    `json:"schema"`
	CheckedAt       string `json:"checked_at"`
	CurrentVersion  string `json:"current_version"`
	Channel         string `json:"channel"`
	UpdateAvailable bool   `json:"update_available"`
	LatestVersion   string `json:"latest_version,omitempty"`
	LatestRevision  string `json:"latest_revision,omitempty"`
	PayloadSHA256   string `json:"payload_sha256,omitempty"`
	ManifestURL     string `json:"manifest_url,omitempty"`
	ReleaseNotesURL string `json:"release_notes_url,omitempty"`
	Phase           string `json:"phase"`
	StageDir        string `json:"stage_dir,omitempty"`
	Error           string `json:"error,omitempty"`
}

func updateConfigPath() string { return filepath.Join(updateRootDir(), "update.json") }
func updateStatePath() string  { return filepath.Join(updateRootDir(), "update-state.json") }

func defaultUpdateConfig() updateConfig {
	return updateConfig{Schema: updateSchema, Channel: "stable", CheckIntervalHours: 6}
}

func stripUTF8BOM(raw []byte) []byte {
	if len(raw) >= 3 && raw[0] == 0xef && raw[1] == 0xbb && raw[2] == 0xbf {
		return raw[3:]
	}
	return raw
}

func loadUpdateConfig() (updateConfig, error) {
	path := updateConfigPath()
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return defaultUpdateConfig(), nil
	}
	if err != nil {
		return updateConfig{}, fmt.Errorf("read update config: %w", err)
	}
	var cfg updateConfig
	if err := json.Unmarshal(stripUTF8BOM(raw), &cfg); err != nil {
		return updateConfig{}, fmt.Errorf("parse update config: %w", err)
	}
	if cfg.Schema != updateSchema {
		return updateConfig{}, fmt.Errorf("unsupported update config schema %d", cfg.Schema)
	}
	cfg.Channel = strings.ToLower(strings.TrimSpace(cfg.Channel))
	if cfg.Channel != "stable" && cfg.Channel != "prerelease" {
		return updateConfig{}, fmt.Errorf("invalid update channel %q", cfg.Channel)
	}
	if cfg.CheckIntervalHours <= 0 {
		cfg.CheckIntervalHours = 6
	}
	return cfg, nil
}

func writeUpdateJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return writeFileAtomic(path, raw)
}

func writeUpdateConfig(cfg updateConfig) error {
	cfg.Schema = updateSchema
	cfg.Channel = strings.ToLower(strings.TrimSpace(cfg.Channel))
	if cfg.Channel != "stable" && cfg.Channel != "prerelease" {
		return fmt.Errorf("invalid update channel %q", cfg.Channel)
	}
	if cfg.CheckIntervalHours <= 0 {
		cfg.CheckIntervalHours = 6
	}
	return writeUpdateJSON(updateConfigPath(), cfg)
}

func writeUpdateState(st updateState) error {
	st.Schema = updateSchema
	if st.CheckedAt == "" {
		st.CheckedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return writeUpdateJSON(updateStatePath(), st)
}

func readUpdateState() (updateState, error) {
	raw, err := os.ReadFile(updateStatePath())
	if err != nil {
		return updateState{}, err
	}
	var st updateState
	if err := json.Unmarshal(stripUTF8BOM(raw), &st); err != nil {
		return updateState{}, err
	}
	return st, nil
}

func manifestURLsForChannel(channel string) []string {
	if strings.EqualFold(strings.TrimSpace(channel), "prerelease") {
		// Как в AIRKS: prerelease смотрит и stable, иначе машина не увидит
		// более новый стабильный выпуск.
		return []string{updateStableManifestURL, updatePrereleaseManifestURL}
	}
	return []string{updateStableManifestURL}
}

func validateUpdateHTTPSURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "https") || u.Host == "" {
		return fmt.Errorf("URL must use HTTPS: %s", raw)
	}
	if u.User != nil || u.Fragment != "" {
		return fmt.Errorf("URL must not contain credentials or fragment: %s", raw)
	}
	return nil
}

func validateUpdateArtifact(a updateArtifact, wantName, manifestVersion string) error {
	if a.Name != wantName {
		return fmt.Errorf("asset name %q, want %q", a.Name, wantName)
	}
	if err := validateUpdateHTTPSURL(a.URL); err != nil {
		return err
	}
	u, _ := url.Parse(a.URL)
	if !strings.EqualFold(u.Hostname(), "github.com") {
		return fmt.Errorf("release asset is not hosted on github.com")
	}
	tag := "air-worker--v" + strings.TrimPrefix(strings.TrimSpace(manifestVersion), "v")
	wantPrefix := "/ruhorh66-rgb/air-plugins/releases/download/" + tag + "/"
	if !strings.HasPrefix(u.Path, wantPrefix) || filepath.Base(u.Path) != wantName {
		return fmt.Errorf("release asset URL is not immutable tag %s", tag)
	}
	if a.Size <= 0 {
		return fmt.Errorf("invalid asset size %d", a.Size)
	}
	sum, err := hex.DecodeString(strings.TrimSpace(a.SHA256))
	if err != nil || len(sum) != sha256.Size {
		return fmt.Errorf("invalid asset sha256")
	}
	return nil
}

func updateSignatureMessage(m updateManifest) []byte {
	parts := []string{
		strconv.Itoa(m.Schema),
		strings.ToLower(strings.TrimSpace(m.Channel)),
		strings.TrimSpace(m.Version),
		strings.TrimSpace(m.PublishedAt),
		strings.ToLower(strings.TrimSpace(m.VCSRevision)),
		strings.ToLower(strings.TrimSpace(m.PayloadSHA256)),
		m.CLI.Name,
		m.CLI.URL,
		strings.ToLower(strings.TrimSpace(m.CLI.SHA256)),
		strconv.FormatInt(m.CLI.Size, 10),
		m.Tray.Name,
		m.Tray.URL,
		strings.ToLower(strings.TrimSpace(m.Tray.SHA256)),
		strconv.FormatInt(m.Tray.Size, 10),
		strings.TrimSpace(m.SigningKeyID),
		strings.TrimSpace(m.ReleaseNotesURL),
	}
	return []byte(strings.Join(parts, "\n"))
}

func verifyUpdateManifestWithKey(m updateManifest, currentVersion, selectedChannel, publicKeyB64, expectedKeyID string) (bool, error) {
	if m.Schema != updateSchema {
		return false, fmt.Errorf("unsupported update manifest schema %d", m.Schema)
	}
	m.Channel = strings.ToLower(strings.TrimSpace(m.Channel))
	selectedChannel = strings.ToLower(strings.TrimSpace(selectedChannel))
	switch selectedChannel {
	case "stable":
		if m.Channel != "stable" {
			return false, fmt.Errorf("stable channel rejects %q manifest", m.Channel)
		}
	case "prerelease":
		if m.Channel != "stable" && m.Channel != "prerelease" {
			return false, fmt.Errorf("prerelease channel rejects %q manifest", m.Channel)
		}
	default:
		return false, fmt.Errorf("invalid selected channel %q", selectedChannel)
	}

	mv, err := parseUpdateSemVersion(m.Version)
	if err != nil {
		return false, fmt.Errorf("manifest version: %w", err)
	}
	if m.Channel == "stable" && len(mv.Pre) != 0 {
		return false, fmt.Errorf("stable manifest contains prerelease version %s", m.Version)
	}
	if m.Channel == "prerelease" && len(mv.Pre) == 0 {
		return false, fmt.Errorf("prerelease manifest contains stable version %s", m.Version)
	}
	cv, err := parseUpdateSemVersion(currentVersion)
	if err != nil {
		return false, fmt.Errorf("current version: %w", err)
	}

	if _, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(m.PublishedAt)); err != nil {
		return false, fmt.Errorf("invalid published_at: %w", err)
	}
	rev, err := hex.DecodeString(strings.TrimSpace(m.VCSRevision))
	if err != nil || len(rev) != 20 {
		return false, fmt.Errorf("invalid vcs_revision")
	}
	payload, err := hex.DecodeString(strings.TrimSpace(m.PayloadSHA256))
	if err != nil || len(payload) != sha256.Size {
		return false, fmt.Errorf("invalid payload_sha256")
	}
	v := strings.TrimPrefix(strings.TrimSpace(m.Version), "v")
	if err := validateUpdateArtifact(m.CLI, "air-worker-"+v+"-windows-x64.exe", m.Version); err != nil {
		return false, fmt.Errorf("cli asset: %w", err)
	}
	if err := validateUpdateArtifact(m.Tray, "air-worker-tray-"+v+"-windows-x64.exe", m.Version); err != nil {
		return false, fmt.Errorf("tray asset: %w", err)
	}
	if m.ReleaseNotesURL != "" {
		if err := validateUpdateHTTPSURL(m.ReleaseNotesURL); err != nil {
			return false, fmt.Errorf("release_notes_url: %w", err)
		}
	}
	if m.SigningKeyID != expectedKeyID {
		return false, fmt.Errorf("unexpected update signing key id %q", m.SigningKeyID)
	}
	pub, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return false, fmt.Errorf("update public key is invalid")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(m.ManifestSig))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false, fmt.Errorf("invalid manifest signature")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), updateSignatureMessage(m), sig) {
		return false, fmt.Errorf("update manifest signature verification failed")
	}
	return compareUpdateSemVersion(mv, cv) > 0, nil
}

func verifyUpdateManifest(m updateManifest, currentVersion, selectedChannel string) (bool, error) {
	return verifyUpdateManifestWithKey(m, currentVersion, selectedChannel, updatePublicKeyB64, updateKeyID)
}

func updateHTTPClient(timeout time.Duration) *http.Client {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Client{Timeout: timeout}
	}
	tr := base.Clone()
	return &http.Client{
		Transport: tr,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 8 {
				return fmt.Errorf("too many update redirects")
			}
			return validateUpdateHTTPSURL(req.URL.String())
		},
	}
}

func fetchUpdateManifest(ctx context.Context, rawURL string) (updateManifest, error) {
	if err := validateUpdateHTTPSURL(rawURL); err != nil {
		return updateManifest{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return updateManifest{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", appName+"/"+version)
	resp, err := updateHTTPClient(25 * time.Second).Do(req)
	if err != nil {
		return updateManifest{}, fmt.Errorf("fetch update manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return updateManifest{}, fmt.Errorf("update manifest HTTP %s", resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxUpdateManifestBytes+1))
	if err != nil {
		return updateManifest{}, fmt.Errorf("read update manifest: %w", err)
	}
	if len(raw) > maxUpdateManifestBytes {
		return updateManifest{}, fmt.Errorf("update manifest too large")
	}
	var m updateManifest
	if err := json.Unmarshal(stripUTF8BOM(raw), &m); err != nil {
		return updateManifest{}, fmt.Errorf("parse update manifest: %w", err)
	}
	return m, nil
}

func checkForAirWorkerUpdate(ctx context.Context) (updateManifest, updateState, error) {
	cfg, err := loadUpdateConfig()
	if err != nil {
		st := updateState{CurrentVersion: version, Phase: "error", Error: err.Error()}
		_ = writeUpdateState(st)
		return updateManifest{}, st, err
	}

	var best updateManifest
	var bestURL string
	var lastErr error
	validFound := false
	for _, source := range manifestURLsForChannel(cfg.Channel) {
		m, err := fetchUpdateManifest(ctx, source)
		if err != nil {
			lastErr = err
			continue
		}
		if _, err = verifyUpdateManifest(m, version, cfg.Channel); err != nil {
			lastErr = err
			continue
		}
		if !validFound || compareUpdateVersionStrings(m.Version, best.Version) > 0 {
			best, bestURL, validFound = m, source, true
		}
	}
	if !validFound {
		if lastErr == nil {
			lastErr = errors.New("no valid update manifest available")
		}
		st := updateState{
			CurrentVersion: version,
			Channel:        cfg.Channel,
			Phase:          "error",
			Error:          lastErr.Error(),
		}
		_ = writeUpdateState(st)
		return updateManifest{}, st, lastErr
	}

	available, err := verifyUpdateManifest(best, version, cfg.Channel)
	st := updateState{
		CurrentVersion:  version,
		Channel:         cfg.Channel,
		LatestVersion:   best.Version,
		LatestRevision:  strings.ToLower(best.VCSRevision),
		PayloadSHA256:   strings.ToLower(best.PayloadSHA256),
		ManifestURL:     bestURL,
		ReleaseNotesURL: best.ReleaseNotesURL,
	}
	if err != nil {
		st.Phase, st.Error = "error", err.Error()
		_ = writeUpdateState(st)
		return best, st, err
	}
	st.UpdateAvailable = available
	if available {
		st.Phase = "available"
	} else {
		st.Phase = "current"
	}
	if err := writeUpdateState(st); err != nil {
		return best, st, err
	}
	return best, st, nil
}

func downloadUpdateArtifact(ctx context.Context, a updateArtifact, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", appName+"/"+version)
	resp, err := updateHTTPClient(10 * time.Minute).Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", a.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s HTTP %s", a.Name, resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	part := dst + ".part"
	_ = os.Remove(part)
	f, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, a.Size+1))
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(part)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(part)
		return closeErr
	}
	if n != a.Size {
		_ = os.Remove(part)
		return fmt.Errorf("%s size mismatch: got %d want %d", a.Name, n, a.Size)
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, a.SHA256) {
		_ = os.Remove(part)
		return fmt.Errorf("%s sha256 mismatch", a.Name)
	}
	_ = os.Remove(dst)
	if err := os.Rename(part, dst); err != nil {
		_ = os.Remove(part)
		return err
	}
	return nil
}

func verifyDownloadedArtifact(path string, a updateArtifact) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if st.IsDir() || st.Size() != a.Size {
		return fmt.Errorf("%s size mismatch", a.Name)
	}
	h, err := sha256File(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(h, a.SHA256) {
		return fmt.Errorf("%s sha256 mismatch", a.Name)
	}
	return nil
}

func stageAirWorkerUpdate(ctx context.Context, m updateManifest) (string, string, error) {
	v := strings.TrimPrefix(strings.TrimSpace(m.Version), "v")
	stage := filepath.Join(updateRootDir(), "staging", v)
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return "", "", err
	}
	cli := filepath.Join(stage, "air-worker.exe")
	tray := filepath.Join(stage, "air-worker-tray.exe")
	if err := downloadUpdateArtifact(ctx, m.CLI, cli); err != nil {
		return "", "", err
	}
	if err := downloadUpdateArtifact(ctx, m.Tray, tray); err != nil {
		return "", "", err
	}
	if err := verifyDownloadedArtifact(cli, m.CLI); err != nil {
		return "", "", err
	}
	if err := verifyDownloadedArtifact(tray, m.Tray); err != nil {
		return "", "", err
	}
	manifestPath := filepath.Join(stage, "update-manifest.json")
	if err := writeUpdateJSON(manifestPath, m); err != nil {
		return "", "", err
	}
	return stage, manifestPath, nil
}

func printUpdateState(st updateState, asJSON bool) {
	if asJSON {
		raw, _ := json.MarshalIndent(st, "", "  ")
		fmt.Println(string(raw))
		return
	}
	fmt.Printf("AirWorker %s · канал %s", st.CurrentVersion, st.Channel)
	switch st.Phase {
	case "unchecked":
		fmt.Print(" · обновления ещё не проверялись")
	case "available":
		fmt.Printf(" · доступно %s", st.LatestVersion)
	case "current":
		fmt.Print(" · актуальная версия")
	case "downloaded":
		fmt.Printf(" · %s скачано и проверено", st.LatestVersion)
	case "handoff":
		fmt.Printf(" · установка %s передана новому бинарнику", st.LatestVersion)
	case "installed":
		fmt.Printf(" · установлено %s", st.LatestVersion)
	case "rolled_back":
		fmt.Printf(" · обновление %s откатилось", st.LatestVersion)
	case "error":
		fmt.Printf(" · ОШИБКА: %s", st.Error)
	default:
		if st.Phase != "" {
			fmt.Printf(" · %s", st.Phase)
		}
	}
	if st.CheckedAt != "" {
		fmt.Printf(" · проверено %s", st.CheckedAt)
	}
	fmt.Print(lineEnding)
}

func cmdUpdate(argv []string) int {
	if len(argv) == 0 {
		fmt.Fprint(os.Stderr, "usage: air-worker update <status|check|download|install|channel>"+lineEnding)
		return 2
	}
	switch strings.ToLower(argv[0]) {
	case "channel":
		cfg, err := loadUpdateConfig()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		if len(argv) == 1 {
			fmt.Println(cfg.Channel)
			return 0
		}
		if len(argv) != 2 {
			return 2
		}
		cfg.Channel = strings.ToLower(strings.TrimSpace(argv[1]))
		if err := writeUpdateConfig(cfg); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		fmt.Printf("канал обновления: %s%s", cfg.Channel, lineEnding)
		return 0

	case "status":
		fs := flag.NewFlagSet("update status", flag.ContinueOnError)
		asJSON := fs.Bool("json", false, "JSON")
		if err := fs.Parse(argv[1:]); err != nil {
			return 2
		}
		cfg, err := loadUpdateConfig()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		st, err := readUpdateState()
		if err != nil {
			st = updateState{Schema: updateSchema, CurrentVersion: version, Channel: cfg.Channel, Phase: "unchecked"}
		}
		st.CurrentVersion = version
		if st.Channel == "" {
			st.Channel = cfg.Channel
		}
		printUpdateState(st, *asJSON)
		return 0

	case "check":
		fs := flag.NewFlagSet("update check", flag.ContinueOnError)
		asJSON := fs.Bool("json", false, "JSON")
		if err := fs.Parse(argv[1:]); err != nil {
			return 2
		}
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		_, st, err := checkForAirWorkerUpdate(ctx)
		printUpdateState(st, *asJSON)
		if err != nil {
			return 1
		}
		return 0

	case "download", "install":
		fs := flag.NewFlagSet("update "+argv[0], flag.ContinueOnError)
		asJSON := fs.Bool("json", false, "JSON")
		if err := fs.Parse(argv[1:]); err != nil {
			return 2
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		m, st, err := checkForAirWorkerUpdate(ctx)
		if err != nil {
			printUpdateState(st, *asJSON)
			return 1
		}
		if !st.UpdateAvailable {
			printUpdateState(st, *asJSON)
			return 0
		}
		stage, manifestPath, err := stageAirWorkerUpdate(ctx, m)
		if err != nil {
			st.Phase, st.Error = "error", err.Error()
			_ = writeUpdateState(st)
			printUpdateState(st, *asJSON)
			return 1
		}
		st.Phase, st.StageDir, st.Error = "downloaded", stage, ""
		_ = writeUpdateState(st)
		if strings.EqualFold(argv[0], "download") {
			printUpdateState(st, *asJSON)
			return 0
		}
		st.Phase = "syncing-host-caches"
		_ = writeUpdateState(st)
		if _, err := syncKnownPluginCaches(m); err != nil {
			st.Phase, st.Error = "error", "host plugin cache sync failed: "+err.Error()
			_ = writeUpdateState(st)
			printUpdateState(st, *asJSON)
			return 1
		}
		st.Phase, st.Error = "handoff", ""
		_ = writeUpdateState(st)
		if err := launchAirWorkerUpdateApply(m, stage, manifestPath, updateInstallHome()); err != nil {
			st.Phase, st.Error = "error", err.Error()
			_ = writeUpdateState(st)
			printUpdateState(st, *asJSON)
			return 1
		}
		printUpdateState(st, *asJSON)
		return 0

	case "_apply":
		return cmdUpdateApply(argv[1:])
	default:
		fmt.Fprintln(os.Stderr, "unknown update command")
		return 2
	}
}
