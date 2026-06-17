// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

//go:build !windows

package cleanroom

import "syscall"

// sysProcAttrDetached returns SysProcAttr that detaches the child process from
// the parent's process group so `tilt up` survives the orchestrator exiting.
// Matches the v1 bash pattern: nohup setsid tilt up & disown
func sysProcAttrDetached() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid: true, // child in its own process group
		Setsid:  true, // detach from controlling terminal
	}
}
