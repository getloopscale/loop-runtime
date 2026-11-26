// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package unit

import (
	"regexp"
	"strings"
	"testing"
)

// Every merge pushes every chart to GHCR, so one transient registry error failed the whole
// artifacts run: run 45447262098 lost a push to `failed to perform "Tag" on destination:
// ...: not found` and passed when re-run unchanged. A push is retried a bounded number of
// times before the run fails.
func TestAChartPushIsRetriedBeforeTheRunFails(t *testing.T) {
	workflow := readRepoFile(t, ".github", "workflows", "release-artifacts.yml")
	start := strings.Index(workflow, "- name: Package + push the charts (OCI)")
	if start < 0 {
		t.Fatal("release-artifacts.yml has no chart push step")
	}
	step := workflow[start:]
	if end := strings.Index(step[1:], "\n      - name:"); end > 0 {
		step = step[:end+1]
	}
	if !regexp.MustCompile(`for attempt in 1 2 3; do\s+helm push "\$tgz"`).MatchString(step) {
		t.Errorf("helm push is not retried up to three times:\n%s", step)
	}
	if !strings.Contains(step, "exit 1") {
		t.Error("the step does not fail once the attempts run out")
	}
}
