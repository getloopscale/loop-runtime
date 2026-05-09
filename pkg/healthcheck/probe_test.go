// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package healthcheck

import (
	"os"
	"testing"
)

// TestMaybeRunAndExit_FlagAbsent — without the flag, returns false and does
// NOT call os.Exit. Verified via the negative case (we'd never return from
// here if MaybeRunAndExit incorrectly called os.Exit).
func TestMaybeRunAndExit_FlagAbsent(t *testing.T) {
	origArgs := os.Args
	os.Args = []string{"operator", "--leader-elect=true"}
	defer func() { os.Args = origArgs }()

	if MaybeRunAndExit("http://127.0.0.1:1/healthz") {
		t.Error("MaybeRunAndExit returned true with no --healthcheck-probe flag")
	}
}

// TestFlagName_Stable — the flag name is a Dockerfile contract; changing it
// silently breaks every operator HEALTHCHECK directive. Lock the value here.
func TestFlagName_Stable(t *testing.T) {
	const expected = "--healthcheck-probe"
	if FlagName != expected {
		t.Errorf("FlagName changed from %q to %q — this breaks every operator Dockerfile HEALTHCHECK. Update Dockerfiles or revert.", expected, FlagName)
	}
}
