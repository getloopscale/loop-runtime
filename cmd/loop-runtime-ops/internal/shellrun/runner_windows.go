// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

//go:build windows

package shellrun

import "os/exec"

// applyProcAttr is a no-op on Windows. Process groups work
// differently (Job Objects); LoopRuntime runs only on Linux/macOS in
// production, so Windows support is best-effort for developer
// laptops. Cancellation still works via cmd.Process.Kill().
func applyProcAttr(cmd *exec.Cmd) {
	// Windows uses CREATE_NEW_PROCESS_GROUP flag via SysProcAttr.
	// Omitted because LoopRuntime doesn't support Windows runtime — this
	// file exists so `go build` doesn't fail on Windows devs.
	_ = cmd
}

// killProcessGroup on Windows falls back to killing only the direct
// child. Grandchild orphan risk remains; document as known limitation.
func killProcessGroup(cmd *exec.Cmd, kill bool) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
