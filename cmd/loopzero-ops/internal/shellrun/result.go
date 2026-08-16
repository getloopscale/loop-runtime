// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package shellrun provides typed subprocess execution for the
// loopzero-ops binary toolbox. It replaces ad-hoc bash invocations
// (`cmd1 | cmd2 && cmd3`) with a Runner interface that emits
// structured logs, metrics, and traces per invocation.
//
// Plan: docs/plans/completed/2026-05-21-bash-to-go-migration-plan.md
// ADR: docs/decisions/2026-05-21-bash-to-go-migration-adr.md (D7+D9)
//
// Phase 1 T1.1 of the bash → Go migration.
package shellrun

import "time"

// Result captures the outcome of a single subprocess invocation.
//
// The schema is part of the loopzero-ops --json contract (per CLAUDE.md
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
	// excess is dropped. For commands that emit large outputs (e.g.,
	// `pulumi stack output --json`), use WithStreamingStdout(io.Writer).
	Stdout string `json:"stdout"`

	// Stderr is the captured standard error. Same MaxBufferSize cap.
	Stderr string `json:"stderr"`

	// ExitCode is the process exit code:
	//   0 — success
	//   1 — usage error (mapped to OPS_USAGE)
	//   2 — build failure (mapped to OPS_BUILD_FAILURE)
	//   3 — deploy failure (mapped to OPS_DEPLOY_FAILURE)
	//   4 — auth error (mapped to OPS_AUTH)
	//   5 — partial failure (mapped to OPS_PARTIAL)
	//   negative — process killed by signal (e.g., -9 for SIGKILL)
	ExitCode int `json:"exit_code"`

	// Duration is the wall-clock time from Run() start to subprocess
	// exit (including signal-handling grace period if cancelled).
	Duration time.Duration `json:"duration_ns"`

	// Truncated indicates Stdout or Stderr exceeded MaxBufferSize.
	// Operators should re-run with WithStreamingStdout if they need
	// the full output.
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
// commands (helmfile sync, pulumi up) produce. Callers needing larger
// output use WithStreamingStdout.
const MaxBufferSize = 10 * 1024 * 1024
