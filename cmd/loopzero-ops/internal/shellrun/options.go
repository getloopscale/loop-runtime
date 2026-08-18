// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package shellrun

import (
	"io"
	"time"
)

// Option configures a Runner invocation. Functional-options pattern
// per CNCF Go convention (used in containerd, etcd, helm SDK).
type Option func(*runOptions)

// runOptions captures all caller-overridable knobs for a Run() call.
// Defaults reflect typical orchestrator usage (no env override, parent
// cwd, no streaming, no stdin).
type runOptions struct {
	env         []string
	cwd         string
	stdin       io.Reader
	stdoutW     io.Writer
	stderrW     io.Writer
	timeout     time.Duration
	envIsolated bool
}

// WithEnv replaces (does NOT extend) the environment variables passed
// to the child process. Use this to scope drill credentials away from
// the operator's shell env (D7' isolation pattern).
//
// To extend rather than replace, callers should manually concatenate:
//
//	env := append(os.Environ(), "EXTRA=value")
//	runner.Run(ctx, "...", []string{}, WithEnv(env))
func WithEnv(env []string) Option {
	return func(o *runOptions) {
		o.env = env
	}
}

// WithEnvIsolated runs the child with an EMPTY environment (no PATH,
// no HOME, nothing). Use sparingly — most commands need at least PATH.
// Designed for security-critical invocations (e.g., signing operations
// where we want to prevent malicious env injection).
func WithEnvIsolated() Option {
	return func(o *runOptions) {
		o.env = []string{}
		o.envIsolated = true
	}
}

// WithCwd sets the working directory for the child process. Default
// is the parent's cwd. Absolute paths recommended for reproducibility.
func WithCwd(dir string) Option {
	return func(o *runOptions) {
		o.cwd = dir
	}
}

// WithStdin attaches a reader as the child's stdin. Useful for piping
// content (Pulumi config via stdin, kubectl apply -f - via stdin).
//
// Reader is read in a goroutine; cancelling the context closes the
// pipe and unblocks the reader.
func WithStdin(r io.Reader) Option {
	return func(o *runOptions) {
		o.stdin = r
	}
}

// WithStreamingStdout streams the child's stdout to the given writer
// in real time, bypassing the MaxBufferSize cap. Result.Stdout will
// be empty when this option is used; the writer is the canonical sink.
//
// Use for commands that emit large output (>10 MiB) like:
//
//	pulumi stack output --json
//	helmfile diff
//	docker save | gzip
func WithStreamingStdout(w io.Writer) Option {
	return func(o *runOptions) {
		o.stdoutW = w
	}
}

// WithStreamingStderr is the stderr equivalent of WithStreamingStdout.
// Common pattern for shell-orchestrator scripts that log to stderr:
//
//	WithStreamingStderr(os.Stderr) // tee child's stderr to ours
func WithStreamingStderr(w io.Writer) Option {
	return func(o *runOptions) {
		o.stderrW = w
	}
}

// WithTimeout sets a hard timeout on the child process. After the
// timeout, SIGTERM is sent; after an additional 5s grace, SIGKILL.
// Per EC-4 fix (D9): kill propagates to the entire process group
// via Setpgid + negative-PID signaling.
//
// Default is no timeout (caller's context.Context drives cancellation).
func WithTimeout(d time.Duration) Option {
	return func(o *runOptions) {
		o.timeout = d
	}
}
