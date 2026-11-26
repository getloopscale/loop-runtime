// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

//go:build unix

package shellrun

import (
	"fmt"
	"os/exec"
	"syscall"
)

// applyProcAttr sets Setpgid=true so the child process starts in a new
// process group. This lets killProcessGroup signal the entire group
// (children of children of children) on cancellation, fixing EC-4
// (Go's exec.CommandContext only kills the direct child by default).
//
// Linux: Setpgid moves child to new PGID == child's PID.
// macOS: same semantic via BSD syscalls.
func applyProcAttr(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessGroup sends a signal to the child's process group. If
// kill=true, sends SIGKILL; otherwise SIGTERM. Uses negative PID
// (group signal) per POSIX kill(2).
//
// Returns an error if the process has already exited (ESRCH) or we
// lack permission (EPERM).
func killProcessGroup(cmd *exec.Cmd, kill bool) error {
	if cmd.Process == nil {
		return fmt.Errorf("shellrun: process not started")
	}
	sig := syscall.SIGTERM
	if kill {
		sig = syscall.SIGKILL
	}
	// Negative PID = signal entire group.
	if err := syscall.Kill(-cmd.Process.Pid, sig); err != nil {
		// Fallback: signal just the direct child if group kill fails
		// (e.g., race where group dissolved).
		_ = cmd.Process.Signal(sig)
		return fmt.Errorf("shellrun: kill group %d sig %v: %w", cmd.Process.Pid, sig, err)
	}
	return nil
}
