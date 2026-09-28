package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

const defaultTrustedHostMarker = `R:\-4-\air-devspace\health-marker.json`

type trustedHostHealthMarker struct {
	PID        int    `json:"pid"`
	State      string `json:"state"`
	UpdatedUTC string `json:"updated_utc"`
}

var (
	trustedHostMarkerPath = defaultTrustedHostMarker
	trustedHostParentPID  = os.Getppid
	trustedHostNow        = time.Now
)

func trustedHostMarker() string {
	if v := strings.TrimSpace(os.Getenv("AIR_COMMANDER_HEALTH_MARKER")); v != "" {
		return v
	}
	return trustedHostMarkerPath
}

func verifyTrustedHostApprovalSource() error {
	raw, err := os.ReadFile(trustedHostMarker())
	if err != nil {
		return fmt.Errorf("trusted host marker unavailable: %w", err)
	}
	var marker trustedHostHealthMarker
	if err := json.Unmarshal(raw, &marker); err != nil {
		return fmt.Errorf("trusted host marker invalid: %w", err)
	}
	parent := trustedHostParentPID()
	if marker.PID <= 0 || parent <= 0 || marker.PID != parent {
		return fmt.Errorf("approval caller pid %d is not current AIR Commander bridge pid %d", parent, marker.PID)
	}
	if !strings.EqualFold(strings.TrimSpace(marker.State), "RUNNING") {
		return fmt.Errorf("AIR Commander bridge state is %q, not RUNNING", marker.State)
	}
	updated, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(marker.UpdatedUTC))
	if err != nil {
		return fmt.Errorf("trusted host marker has invalid updated_utc: %w", err)
	}
	age := trustedHostNow().UTC().Sub(updated.UTC())
	if age < -5*time.Minute || age > 2*time.Minute {
		return fmt.Errorf("trusted host marker is stale: age=%s", age.Round(time.Second))
	}
	return nil
}
