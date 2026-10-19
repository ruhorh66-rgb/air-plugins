package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	nativeReleaseSliceR02 = "R0.11.9-02"
	nativeReleaseSliceR03 = "R0.11.9-03"
	nativeReleaseSliceR04 = "R0.11.9-04"
)

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
	if !state.HasActiveStep || state.ActiveStep.ID != state.NextStep {
		return fmt.Errorf("native release step identity differs from active plan: %q", state.NextStep)
	}
	if state.ActiveStep.ID != nativeReleaseSliceR02 && state.ActiveStep.ID != nativeReleaseSliceR03 && state.ActiveStep.ID != nativeReleaseSliceR04 {
		return fmt.Errorf("native loop currently supports only active %s, %s or %s; found %q",
			nativeReleaseSliceR02, nativeReleaseSliceR03, nativeReleaseSliceR04, state.NextStep)
	}
	if state.ActiveVersion != "0.11.9" || candidateVersion != "0.11.9" || state.ActiveStep.Status != "open" ||
		(state.ActiveStep.Kind != "source" && !(state.ActiveStep.ID == nativeReleaseSliceR04 && state.ActiveStep.Kind == "test")) {
		return fmt.Errorf("%s requires an open source slice and an exact 0.11.9 candidate (plan=%q binary=%q status=%q kind=%q)", state.ActiveStep.ID, state.ActiveVersion, candidateVersion, state.ActiveStep.Status, state.ActiveStep.Kind)
	}
	return nil
}

func cmdReleaseSliceLoop(root string, cfg runConfig, state releaseSlicesSummary) int {
	if err := releaseSliceLoopSupported(state, version); err != nil {
		line("ОТКАЗ: " + err.Error())
		return 2
	}
	switch state.ActiveStep.ID {
	case nativeReleaseSliceR02:
		return runReleaseSliceR02(root, cfg, state)
	case nativeReleaseSliceR03:
		return runNativeReleaseSliceR03(root, state)
	case nativeReleaseSliceR04:
		return runNativeReleaseSliceR04(root, state)
	default:
		line("ОТКАЗ: unsupported native release slice " + state.ActiveStep.ID)
		return 2
	}
}

func runReleaseSliceR02(root string, _ runConfig, state releaseSlicesSummary) int {
	return runNativeReleaseSliceR02(root, state)
}
