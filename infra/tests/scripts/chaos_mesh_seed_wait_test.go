// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package scripts

import (
	"os"
	"regexp"
	"testing"
)

// The chaos-mesh seed counts Running pods right after `helm upgrade --install`. Without
// --wait helm returns as soon as the objects are submitted, so a cold install counted
// 0 of 6 two seconds after deploying and failed the seed, while 45 seconds later all 6
// were Running (#409). The install must wait for the rollout it then verifies.
func TestTheChaosMeshSeedWaitsForItsRolloutBeforeCounting(t *testing.T) {
	raw, err := os.ReadFile("../../scripts/local/seed-chaos-mesh-canonical.sh")
	if err != nil {
		t.Fatalf("read seed-chaos-mesh-canonical.sh: %v", err)
	}
	install := regexp.MustCompile(`(?s)\nhelm upgrade --install chaos-mesh .*?\n\n`).Find(raw)
	if install == nil {
		t.Fatal("the seed runs no `helm upgrade --install chaos-mesh`")
	}
	if !regexp.MustCompile(`--wait\b`).Match(install) {
		t.Errorf("the chaos-mesh install does not pass --wait:\n%s", install)
	}
	if !regexp.MustCompile(`--timeout[ =]\S+`).Match(install) {
		t.Errorf("the chaos-mesh install waits with no explicit --timeout:\n%s", install)
	}
}
