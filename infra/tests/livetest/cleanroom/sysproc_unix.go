// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

//go:build !windows

package cleanroom

import "syscall"

// sysProcAttrDetached returns SysProcAttr that detaches the child process from
// the parent's process group so `tilt up` survives the orchestrator exiting.
// Matches the v1 bash pattern: nohup setsid tilt up & disown
func sysProcAttrDetached() *syscall.SysProcAttr {
	// Setsid alone: a new session is also a new process group. Adding Setpgid made
	// the child call setpgid() after setsid(), which the kernel refuses a session
	// leader with EPERM — tilt never started (#416).
	return &syscall.SysProcAttr{Setsid: true}
}
