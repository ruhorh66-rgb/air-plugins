package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const selfLearningTransferSchema = "air-worker.self-learning-transfer/v1"

// transferAirWorkerSelfLearningOwner performs an explicit owner migration while
// retaining the existing shared runtime and refusing any ambiguous product state.
// The destination must already be bootstrapped against that exact runtime.
func transferAirWorkerSelfLearningOwner(targetProduct string) (airWorkerSelfLearningSelector, error) {
	var zero airWorkerSelfLearningSelector
	root, err := normalizeLearnProduct(targetProduct)
	if err != nil {
		return zero, err
	}
	settings, on, err := readSharedLearningSettings(root)
	if err != nil {
		return zero, err
	}
	if !on {
		return zero, errors.New("target product has no shared-learning configuration")
	}
	current, enabled, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !enabled {
		if err == nil {
			err = errors.New("active self-learning owner is not enabled")
		}
		return zero, err
	}
	if sameLearningPath(root, current.Selector.ProductRoot) {
		return current.Selector, nil
	}
	if settings.ProductID != current.Settings.ProductID || !sameLearningPath(settings.RuntimeRoot, current.Settings.RuntimeRoot) {
		return zero, errors.New("target product must use the existing product id and runtime")
	}
	unlock, err := acquireSharedLearningBootstrapLock(root, settings.RuntimeRoot, 30*time.Second)
	if err != nil {
		return zero, err
	}
	defer unlock()
	current, enabled, err = activeAirWorkerSelfLearningOwner()
	if err != nil || !enabled {
		if err == nil {
			err = errors.New("active self-learning owner changed during transfer")
		}
		return zero, err
	}
	settings, on, err = readSharedLearningSettings(root)
	if err != nil || !on || settings.ProductID != current.Settings.ProductID || !sameLearningPath(settings.RuntimeRoot, current.Settings.RuntimeRoot) {
		if err == nil {
			err = errors.New("target owner identity changed during transfer")
		}
		return zero, err
	}
	intent, found, err := readSharedLearningBootstrapIntent(settings.RuntimeRoot)
	if err != nil || !found || intent.Status != "complete" || intent.ProductID != settings.ProductID {
		if err == nil {
			err = errors.New("completed shared-learning bootstrap proof is missing")
		}
		return zero, err
	}
	if err := verifySharedBootstrapState(root, settings, intent); err != nil {
		return zero, fmt.Errorf("target runtime verification: %w", err)
	}
	configSHA, err := selfLearningConfigSHA(root)
	if err != nil {
		return zero, err
	}
	auditPath := filepath.Join(root, "docs", "receipts", "SELF_LEARNING_OWNER_TRANSFER.json")
	if err := os.MkdirAll(filepath.Dir(auditPath), 0o755); err != nil {
		return zero, err
	}
	if _, err := os.Lstat(auditPath); err == nil {
		return zero, errors.New("owner transfer audit receipt already exists; refusing overwrite")
	} else if !errors.Is(err, os.ErrNotExist) {
		return zero, err
	}
	receipt := struct {
		Schema           string `json:"schema"`
		Status           string `json:"status"`
		PreviousProduct  string `json:"previous_product_root"`
		TargetProduct    string `json:"target_product_root"`
		ProductID        string `json:"product_id"`
		RuntimeRoot      string `json:"runtime_root"`
		ConfigSHA256     string `json:"target_config_sha256"`
		TransferredAtUTC string `json:"transferred_at_utc"`
	}{selfLearningTransferSchema, "transferred", current.Selector.ProductRoot, root, settings.ProductID, settings.RuntimeRoot, configSHA, time.Now().UTC().Format(time.RFC3339Nano)}
	raw, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return zero, err
	}
	if err := writeFileAtomicDurable(auditPath, append(raw, '\n')); err != nil {
		return zero, err
	}
	next := current.Selector
	next.ProductRoot = root
	next.ProductID = settings.ProductID
	next.RuntimeRoot = settings.RuntimeRoot
	next.ConfigSHA256 = configSHA
	next.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	next.DisabledAt = ""
	next.DisableCause = ""
	if err := writeAirWorkerSelfLearningSelector(next); err != nil {
		return zero, err
	}
	readback, found, err := readAirWorkerSelfLearningSelector()
	if err != nil || !found || !readback.Enabled || !sameLearningPath(readback.ProductRoot, root) || !sameLearningPath(readback.RuntimeRoot, settings.RuntimeRoot) || readback.ConfigSHA256 != configSHA {
		_ = writeAirWorkerSelfLearningSelector(current.Selector)
		if err == nil {
			err = errors.New("owner transfer selector readback mismatch")
		}
		return zero, err
	}
	active, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on || !sameLearningPath(active.Selector.ProductRoot, root) {
		_ = writeAirWorkerSelfLearningSelector(current.Selector)
		if err == nil {
			err = errors.New("transferred owner is not active")
		}
		return zero, err
	}
	stored, err := os.ReadFile(auditPath)
	if err != nil || !bytes.Equal(stored, append(raw, '\n')) {
		_ = writeAirWorkerSelfLearningSelector(current.Selector)
		if err == nil {
			err = errors.New("owner transfer receipt readback mismatch")
		}
		return zero, err
	}
	return readback, nil
}
