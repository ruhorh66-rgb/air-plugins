package main

import (
	"fmt"

	"github.com/ruhorh66-rgb/air-modules/learning"
)

// R03 in the first 0.11.9 micro-release is a signed historical checkpoint of
// reviewer/APPLIED, NOT a forever-valid grant. The published module already
// owns its own apply/rollback serialization. The AirWorker core exclusively
// owns release-slices status transitions with exact plan CAS.
//
// Under the approved LPR v0.1.0 scope no cross-product/module mutation API is
// used or fabricated. Fresh R04 use MUST independently revalidate the current
// module index/APPLIED/current target and operation effect; R03 status alone
// has no authority to credit use.
func requireR03PublishedModule() error {
	if learning.Version != "0.1.0" {
		return fmt.Errorf("R03 requires the published shared-learning v0.1.0 contract; actual %s", learning.Version)
	}
	return nil
}
