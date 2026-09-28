//go:build !windows

package main

import (
	"fmt"
	"os"
)

func launchAirWorkerUpdateApply(m updateManifest, stage, manifestPath, home string) error {
	return fmt.Errorf("AirWorker transactional self-update is implemented for Windows")
}

func cmdUpdateApply(argv []string) int {
	fmt.Fprintln(os.Stderr, "AirWorker transactional self-update is implemented for Windows")
	return 2
}
