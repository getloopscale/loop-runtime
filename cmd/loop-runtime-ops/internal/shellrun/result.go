// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package shellrun provides typed subprocess execution for the
// loop-runtime-ops binary toolbox: a Runner interface (with a MockRunner for tests)
// that captures output, logs each invocation with slog, and kills the whole
// process group on cancellation.
//
// ADR: docs/decisions.md (D7+D9).
package shellrun

import "time"

// Result captures the outcome of a single subprocess invocation.
//
// The schema is part of the loop-runtime-ops --json contract (per CLAUDE.md
// Rule 5): fields can be added in future versions but cannot be
// renamed or removed without a major version bump.
type Result struct {
	// Command is the canonical command line for log/audit purposes.
	// Format: "name arg1 arg2 ...". Sensitive args (e.g., tokens)
	// are NOT redacted here — callers using shellrun for credentialed
	// commands MUST log Command at debug level only.
	Command string `json:"command"`

	// Stdout is the captured standard output. Limited to MaxBufferSize
	// (default 10 MiB); larger output triggers Truncated=true and the
	// excess is dropped.
	Stdout string `json:"stdout"`

	// Stderr is the captured standard error. Same MaxBufferSize cap.
	Stderr string `json:"stderr"`

	// ExitCode is the child process's own exit code, unmapped: 0 is success,
	// any other value is whatever the child returned, and a negative value
	// means the process was killed by a signal. Callers that report through
	// loop-runtime-ops' exit-code contract map it themselves.
	ExitCode int `json:"exit_code"`

	// Duration is the wall-clock time from Run() start to subprocess
	// exit (including signal-handling grace period if cancelled).
	Duration time.Duration `json:"duration_ns"`

	// Truncated indicates Stdout or Stderr exceeded MaxBufferSize.
	Truncated bool `json:"truncated"`
}

// IsSuccess reports whether the command exited with code 0.
func (r *Result) IsSuccess() bool {
	return r.ExitCode == 0
}

// IsSignalKilled reports whether the process was terminated by a signal
// (negative exit code in Unix convention).
func (r *Result) IsSignalKilled() bool {
	return r.ExitCode < 0
}

// MaxBufferSize bounds the captured Stdout/Stderr to prevent OOM on
// pathological output. 10 MiB matches what typical orchestrator
// commands (helmfile sync, pulumi up) produce.
const MaxBufferSize = 10 * 1024 * 1024
