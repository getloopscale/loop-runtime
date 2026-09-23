// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"strings"
	"testing"
)

// A MISSING field and an UNSUPPORTED value are different errors and used to
// share one message: `loopzero: runtime "" is not supported`. It names what the
// user did NOT write as if they had written it wrong, and does not say what to
// write (B-154).
func TestMissingRuntimeSaysTheFieldIsMissingAndWhatToWrite(t *testing.T) {
	c := (&unsupportedStrategy{runtime: ""}).failStep("validate-workspace")
	if c == nil {
		t.Fatal("no step emitted")
	}
	msg := strings.Join(c.Args, "\n")

	for _, required := range []string{"runtime", "loopzero.yaml", "does not declare"} {
		if !strings.Contains(msg, required) {
			t.Errorf("the missing-field message does not mention %q:\n%s", required, msg)
		}
	}
	if !strings.Contains(msg, "python") || !strings.Contains(msg, "node") {
		t.Errorf("the message does not say what to write:\n%s", msg)
	}
	// What separates this case from the other: it must not accuse a value nobody gave.
	if strings.Contains(msg, `runtime "" is not supported`) {
		t.Errorf("a missing field is still reported as an unsupported value:\n%s", msg)
	}
}

// The counterpart: a genuinely unsupported value is still reported as such,
// naming the value. Without this assertion, "fixing" the missing case could
// erase the distinction in the other direction.
func TestUnsupportedRuntimeStillNamesTheValue(t *testing.T) {
	c := (&unsupportedStrategy{runtime: "rust"}).failStep("validate-workspace")
	if c == nil {
		t.Fatal("no step emitted")
	}
	if !strings.Contains(strings.Join(c.Args, "\n"), "rust") {
		t.Errorf("the message does not name the refused value:\n%s", strings.Join(c.Args, "\n"))
	}
}
