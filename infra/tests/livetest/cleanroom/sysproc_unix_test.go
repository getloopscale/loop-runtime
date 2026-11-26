// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

//go:build !windows

package cleanroom

import (
	"os/exec"
	"testing"
)

// Phase 3 starts `tilt up` detached. Asking for a new session AND a new process group
// makes the child call setsid() and then setpgid(), and a session leader may not
// change its process group — the kernel answers EPERM, so the start failed with
// `fork/exec /usr/local/bin/tilt: operation not permitted` on every run that reached
// phase 3 (#416). A detached child must actually start.
func TestADetachedChildStarts(t *testing.T) {
	cmd := exec.Command("true")
	cmd.SysProcAttr = sysProcAttrDetached()

	if err := cmd.Start(); err != nil {
		t.Fatalf("start a detached child: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("detached child: %v", err)
	}
}
