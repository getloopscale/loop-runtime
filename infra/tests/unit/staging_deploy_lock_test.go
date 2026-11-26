// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package unit

import (
	"regexp"
	"testing"
)

// The deploy timer fires every few minutes; while a rebuild holds the platform lock it has
// nothing to do, and that skip is not a failure of the unit.
func TestADeploySkippedForTheRebuildsLockIsNotAFailure(t *testing.T) {
	unit := readRepoFile(t, "infra", "staging", "systemd", "loopruntime-staging-deploy.service")
	m := regexp.MustCompile(`flock --nonblock --conflict-exit-code (\d+) `).FindStringSubmatch(unit)
	if m == nil {
		t.Fatal("the deploy unit's flock exits 1 on a held lock, the code of a real failure")
	}
	if !regexp.MustCompile(`(?m)^SuccessExitStatus=` + m[1] + `$`).MatchString(unit) {
		t.Errorf("the deploy unit does not count flock's conflict exit code %s as success", m[1])
	}
}
