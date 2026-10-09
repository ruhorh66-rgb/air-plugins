package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestN095SharedLearnDiscoveryAndRequiredProduct(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"-h"}, {"help"}} {
		code, output := captureLoopOutput(t, func() int { return cmdLearn(args) })
		if code != 0 || !strings.Contains(output, "self") || !strings.Contains(output, "status") {
			t.Fatalf("learn root discovery failed: args=%v code=%d output=%q", args, code, output)
		}
	}
	for _, action := range []string{"status", "paths", "index", "load", "finalize", "pending", "summary", "events"} {
		code, output := captureLoopOutput(t, func() int {
			return cmdLearn([]string{action})
		})
		if code != 2 {
			t.Fatalf("%s no-product exit code=%d; output=%s", action, code, output)
		}
		var data struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal([]byte(output), &data); err != nil || !strings.Contains(data.Error, "-product is required") {
			t.Fatalf("%s missing-context message is not machine readable: %v %q", action, err, output)
		}
		helpCode, helpOutput := captureLoopOutput(t, func() int {
			return cmdLearn([]string{action, "-help"})
		})
		if helpCode != 0 || !strings.Contains(helpOutput, "-product") {
			t.Fatalf("%s action help failed: code=%d output=%q", action, helpCode, helpOutput)
		}
	}
}
func TestN095SharedLearnProductQualifiedStillRoutesToModule(t *testing.T) {
	product, _ := sharedProductFixture(t, false)
	code, output := captureLoopOutput(t, func() int {
		return cmdLearn([]string{"status", "-product", product, "-json"})
	})
	if code != 0 || !strings.Contains(output, "events_path") || strings.Contains(output, "unknown learn action") {
		t.Fatalf("module route lost: code=%d output=%q", code, output)
	}
}
