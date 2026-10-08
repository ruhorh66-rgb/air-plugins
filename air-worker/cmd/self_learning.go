package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	airWorkerSelfLearningSchema = "air-worker.self-learning/v1"
	airWorkerSelfLearningFile   = "air-worker-self-learning.json"
)

type airWorkerSelfLearningSelector struct {
	Schema       string `json:"schema"`
	Enabled      bool   `json:"enabled"`
	ProductRoot  string `json:"product_root"`
	ProductID    string `json:"product_id"`
	RuntimeRoot  string `json:"runtime_root"`
	ConfigSHA256 string `json:"config_sha256"`
	UpdatedAt    string `json:"updated_at"`
	DisabledAt   string `json:"disabled_at,omitempty"`
	DisableCause string `json:"disable_cause,omitempty"`
}

type selfLearningOwner struct {
	Selector airWorkerSelfLearningSelector
	Settings sharedLearningSettings
}

func airWorkerSelfLearningPath() string {
	return filepath.Join(hookStateDir(), airWorkerSelfLearningFile)
}

func decodeAirWorkerSelfLearningSelector(raw []byte) (airWorkerSelfLearningSelector, error) {
	var s airWorkerSelfLearningSelector
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return s, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return s, errors.New("self-learning selector has more than one JSON value")
		}
		return s, err
	}
	if s.Schema != airWorkerSelfLearningSchema ||
		strings.TrimSpace(s.ProductID) == "" ||
		!filepath.IsAbs(strings.TrimSpace(s.ProductRoot)) ||
		!filepath.IsAbs(strings.TrimSpace(s.RuntimeRoot)) ||
		len(strings.TrimSpace(s.ConfigSHA256)) != 64 {
		return s, errors.New("invalid AirWorker self-learning selector identity")
	}
	s.ProductRoot = filepath.Clean(s.ProductRoot)
	s.RuntimeRoot = filepath.Clean(s.RuntimeRoot)
	return s, nil
}

func readAirWorkerSelfLearningSelector() (airWorkerSelfLearningSelector, bool, error) {
	path := airWorkerSelfLearningPath()
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return airWorkerSelfLearningSelector{}, false, nil
	}
	if err != nil {
		return airWorkerSelfLearningSelector{}, true, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return airWorkerSelfLearningSelector{}, true, errors.New("AirWorker self-learning selector is not an ordinary regular file")
	}
	if target, linkErr := os.Readlink(path); linkErr == nil {
		return airWorkerSelfLearningSelector{}, true, fmt.Errorf("AirWorker self-learning selector must not be a link/reparse entry (target %q)", target)
	}
	raw, err := readLearningBounded(path, 64*1024)
	if err != nil {
		return airWorkerSelfLearningSelector{}, true, err
	}
	s, err := decodeAirWorkerSelfLearningSelector(raw)
	return s, true, err
}

func selfLearningConfigSHA(product string) (string, error) {
	raw, err := readLearningBounded(filepath.Join(product, sharedLearningConfigFile), sharedLearningMaxBytes)
	if err != nil {
		return "", err
	}
	return learnSHA(raw), nil
}

func activeAirWorkerSelfLearningOwner() (selfLearningOwner, bool, error) {
	sel, found, err := readAirWorkerSelfLearningSelector()
	if err != nil || !found || !sel.Enabled {
		return selfLearningOwner{Selector: sel}, found && sel.Enabled, err
	}
	root, err := normalizeLearnProduct(sel.ProductRoot)
	if err != nil {
		return selfLearningOwner{Selector: sel}, true, err
	}
	settings, on, err := readSharedLearningSettings(root)
	if err != nil {
		return selfLearningOwner{Selector: sel}, true, err
	}
	if !on {
		return selfLearningOwner{Selector: sel}, true, errors.New("AirWorker self-learning selector points to a product without shared-learning configuration")
	}
	if settings.ProductID != sel.ProductID || !sameLearningPath(settings.RuntimeRoot, sel.RuntimeRoot) {
		return selfLearningOwner{Selector: sel}, true, errors.New("AirWorker self-learning selector and shared-learning owner identity disagree")
	}
	configSHA, err := selfLearningConfigSHA(root)
	if err != nil {
		return selfLearningOwner{Selector: sel}, true, err
	}
	if !strings.EqualFold(configSHA, sel.ConfigSHA256) {
		return selfLearningOwner{Selector: sel}, true, errors.New("AirWorker self-learning configuration SHA changed; explicit re-enable is required")
	}
	sel.ProductRoot = root
	return selfLearningOwner{Selector: sel, Settings: settings}, true, nil
}

func writeAirWorkerSelfLearningSelector(s airWorkerSelfLearningSelector) error {
	if err := os.MkdirAll(filepath.Dir(airWorkerSelfLearningPath()), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomicDurable(airWorkerSelfLearningPath(), append(raw, '\n'))
}

func cmdLearnSelf(argv []string) int {
	if len(argv) == 0 {
		fmt.Fprintln(os.Stderr, "usage: air-worker learn self enable|status|disable|rollback ...")
		return 2
	}
	switch argv[0] {
	case "enable":
		return cmdLearnSelfEnable(argv[1:])
	case "status":
		return cmdLearnSelfStatus(argv[1:])
	case "disable":
		return cmdLearnSelfDisable(argv[1:], "disabled")
	case "rollback":
		return cmdLearnSelfDisable(argv[1:], "rollback-to-0.11.8-behavior")
	default:
		fmt.Fprintf(os.Stderr, "unknown learn self action %q\n", argv[0])
		return 2
	}
}

func cmdLearnSelfEnable(argv []string) int {
	fs := flag.NewFlagSet("learn self enable", flag.ContinueOnError)
	product := fs.String("product", "", "AirWorker product GitRoot")
	runtimeRoot := fs.String("runtime-root", "", "absolute persistent AirWorker learning runtime root")
	productID := fs.String("product-id", "air-worker", "stable AirWorker learning product id")
	asJSON := fs.Bool("json", true, "machine-readable JSON")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "learn self enable: unexpected positional arguments")
		return 2
	}
	if strings.TrimSpace(*runtimeRoot) == "" {
		fmt.Fprintln(os.Stderr, "learn self enable: -runtime-root is required")
		return 2
	}
	if old, found, err := readAirWorkerSelfLearningSelector(); err != nil {
		fmt.Fprintln(os.Stderr, "learn self enable:", err)
		return 2
	} else if found {
		root, rootErr := filepath.Abs(*product)
		if rootErr != nil {
			fmt.Fprintln(os.Stderr, "learn self enable:", rootErr)
			return 2
		}
		runtime, runtimeErr := filepath.Abs(*runtimeRoot)
		if runtimeErr != nil {
			fmt.Fprintln(os.Stderr, "learn self enable:", runtimeErr)
			return 2
		}
		if !sameLearningPath(old.ProductRoot, root) || !sameLearningPath(old.RuntimeRoot, runtime) || old.ProductID != strings.TrimSpace(*productID) {
			fmt.Fprintln(os.Stderr, "learn self enable: existing selector has a different identity; disable/rollback does not authorize retargeting")
			return 2
		}
	}
	report, err := initSharedLearning(*product, *runtimeRoot, *productID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn self enable:", err)
		return 2
	}
	root, err := normalizeLearnProduct(*product)
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn self enable:", err)
		return 2
	}
	settings, on, err := readSharedLearningSettings(root)
	if err != nil || !on {
		if err == nil {
			err = errors.New("shared-learning configuration is not active after bootstrap")
		}
		fmt.Fprintln(os.Stderr, "learn self enable:", err)
		return 2
	}
	configSHA, err := selfLearningConfigSHA(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn self enable:", err)
		return 2
	}
	sel := airWorkerSelfLearningSelector{
		Schema: airWorkerSelfLearningSchema, Enabled: true, ProductRoot: root,
		ProductID: settings.ProductID, RuntimeRoot: settings.RuntimeRoot, ConfigSHA256: configSHA,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := writeAirWorkerSelfLearningSelector(sel); err != nil {
		fmt.Fprintln(os.Stderr, "learn self enable:", err)
		return 2
	}
	doc := map[string]any{"schema": airWorkerSelfLearningSchema, "status": "enabled", "selector": sel, "bootstrap": report}
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(doc)
	} else {
		fmt.Printf("enabled %s -> %s\n", sel.ProductRoot, sel.RuntimeRoot)
	}
	return 0
}

func cmdLearnSelfStatus(argv []string) int {
	fs := flag.NewFlagSet("learn self status", flag.ContinueOnError)
	asJSON := fs.Bool("json", true, "machine-readable JSON")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		return 2
	}
	sel, found, err := readAirWorkerSelfLearningSelector()
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn self status:", err)
		return 2
	}
	status := "unconfigured"
	var ownerStatus any
	if found {
		status = "disabled"
		if sel.Enabled {
			owner, on, ownerErr := activeAirWorkerSelfLearningOwner()
			if ownerErr != nil {
				fmt.Fprintln(os.Stderr, "learn self status:", ownerErr)
				return 2
			}
			if !on {
				fmt.Fprintln(os.Stderr, "learn self status: enabled selector did not resolve an active owner")
				return 2
			}
			status = "enabled"
			ownerStatus = map[string]any{
				"product_id": owner.Settings.ProductID, "runtime_root": owner.Settings.RuntimeRoot,
				"managed_skill_prefix": owner.Settings.ManagedSkillPrefix,
			}
		}
	}
	doc := map[string]any{"schema": airWorkerSelfLearningSchema, "status": status, "configured": found}
	if found {
		doc["selector"] = sel
	}
	if ownerStatus != nil {
		doc["owner"] = ownerStatus
	}
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(doc)
	} else {
		fmt.Println(status)
	}
	return 0
}

func cmdLearnSelfDisable(argv []string, cause string) int {
	fs := flag.NewFlagSet("learn self disable", flag.ContinueOnError)
	asJSON := fs.Bool("json", true, "machine-readable JSON")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		return 2
	}
	sel, found, err := readAirWorkerSelfLearningSelector()
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn self disable:", err)
		return 2
	}
	if !found {
		if *asJSON {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"schema": airWorkerSelfLearningSchema, "status": "unconfigured"})
		} else {
			fmt.Println("unconfigured")
		}
		return 0
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	sel.Enabled = false
	sel.UpdatedAt = now
	sel.DisabledAt = now
	sel.DisableCause = cause
	if err := writeAirWorkerSelfLearningSelector(sel); err != nil {
		fmt.Fprintln(os.Stderr, "learn self disable:", err)
		return 2
	}
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"schema": airWorkerSelfLearningSchema, "status": cause, "selector": sel})
	} else {
		fmt.Println(cause)
	}
	return 0
}

func mergeLearningContext(parts ...string) string {
	var clean []string
	for _, part := range parts {
		if text := strings.TrimSpace(part); text != "" {
			clean = append(clean, text)
		}
	}
	return strings.Join(clean, "\n\n")
}

func airWorkerSelfLearningContext(in hookInput) (string, string, bool, error) {
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil {
		return "", "", on, sharedHookFailure("self-selector", strings.TrimSpace(in.RunID), err)
	}
	if !on {
		return "", "", false, nil
	}
	handled, res, err := sharedLearningContext(owner.Selector.ProductRoot, in)
	if err != nil {
		return owner.Selector.ProductRoot, "", true, err
	}
	if !handled {
		return owner.Selector.ProductRoot, "", true, sharedHookFailure("self-context", strings.TrimSpace(in.RunID), errors.New("enabled AirWorker self-learning owner did not select shared learning"))
	}
	if err := captureAirWorkerSelfLearningContextReceipt(owner, in); err != nil {
		return owner.Selector.ProductRoot, "", true, sharedHookFailure("self-context-receipt", selfLearningRunID(in), err)
	}
	if strings.TrimSpace(res.Context) == "" {
		return owner.Selector.ProductRoot, "", true, nil
	}
	return owner.Selector.ProductRoot, "AIRWORKER SELF-LEARNING (procedures, not permissions):\n" + res.Context, true, nil
}
