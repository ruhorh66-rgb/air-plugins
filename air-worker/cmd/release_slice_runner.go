package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const nativeReleaseSliceR02 = "R0.11.9-02"

func releaseSlicesExpected(productRoot string) bool {
	for _, modPath := range []string{filepath.Join(productRoot, "go.mod"), filepath.Join(productRoot, "cmd", "go.mod")} {
		raw, err := os.ReadFile(modPath)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(raw), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "module" && fields[1] == "airos/air-worker" {
				return true
			}
		}
	}
	selector, found, err := readAirWorkerSelfLearningSelector()
	return found && err == nil && selector.ProductID == "air-worker" && sameLearningPath(selector.ProductRoot, productRoot)
}

func releaseSliceLoopSupported(state releaseSlicesSummary, candidateVersion string) error {
	if !state.Found {
		return fmt.Errorf("release-slices.json is not present")
	}
	if len(state.Problems) != 0 {
		return fmt.Errorf("release-slices.json has %d validation problem(s)", len(state.Problems))
	}
	if !state.HasActiveStep || state.NextStep != nativeReleaseSliceR02 || state.ActiveStep.ID != nativeReleaseSliceR02 {
		return fmt.Errorf("native loop currently supports only active %s; found %q", nativeReleaseSliceR02, state.NextStep)
	}
	if state.ActiveVersion != "0.11.9" || candidateVersion != "0.11.9" || state.ActiveStep.Status != "open" || state.ActiveStep.Kind != "source" {
		return fmt.Errorf("%s requires an open source slice and an exact 0.11.9 candidate (plan=%q binary=%q status=%q kind=%q)", nativeReleaseSliceR02, state.ActiveVersion, candidateVersion, state.ActiveStep.Status, state.ActiveStep.Kind)
	}
	return nil
}

func cmdReleaseSliceLoop(root string, cfg runConfig, state releaseSlicesSummary) int {
	if err := releaseSliceLoopSupported(state, version); err != nil {
		line("ОТКАЗ: " + err.Error())
		return 2
	}
	return runReleaseSliceR02(root, cfg, state)
}

func runReleaseSliceR02(root string, _ runConfig, state releaseSlicesSummary) int {
	return runNativeReleaseSliceR02(root, state)
}
