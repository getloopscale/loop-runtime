// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package shellrun

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Runner executes subprocesses with structured logging, timeout
// enforcement, and process-group kill semantics.
//
// Implementations:
//   - DefaultRunner — production impl using os/exec
//   - MockRunner    — test impl in mock.go
//
// Per ADR D9: cancellation kills the entire process group (Setpgid +
// negative-PID signaling) so grandchildren (helmfile→helm→kubectl)
// don't orphan.
type Runner interface {
	// Run executes the command and returns the full Result.
	Run(ctx context.Context, name string, args []string, opts ...Option) (*Result, error)
}

// DefaultRunner is the production Runner. It spawns processes via
// os/exec with process-group isolation for proper cancellation.
type DefaultRunner struct {
	// Logger is the structured logger used for invocation start/end
	// events. If nil, slog.Default() is used.
	Logger *slog.Logger

	// GracePeriod is the time between SIGTERM and SIGKILL on context
	// cancellation. Default: 5 seconds. Bash equivalent: `kill -TERM;
	// sleep 5; kill -KILL`.
	GracePeriod time.Duration
}

// NewDefault constructs a DefaultRunner with sensible defaults.
func NewDefault() *DefaultRunner {
	return &DefaultRunner{
		Logger:      slog.Default(),
		GracePeriod: 5 * time.Second,
	}
}

// Run implements Runner.Run.
func (r *DefaultRunner) Run(ctx context.Context, name string, args []string, opts ...Option) (*Result, error) {
	opt := runOptions{}
	for _, o := range opts {
		o(&opt)
	}

	logger := r.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Build canonical command string for logs (NOT for shell execution
	// — exec.Command splits args safely; this is display-only).
	command := name
	if len(args) > 0 {
		command = name + " " + strings.Join(args, " ")
	}

	// nosec G204 — shellrun's purpose IS to run variable subprocesses;
	// callers are responsible for input validation at the orchestrator
	// layer (e.g., loopruntime-ops bootstrap drill-helmfile validates its
	// args before invoking shellrun).
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // intentional variable subprocess

	if opt.cwd != "" {
		cmd.Dir = opt.cwd
	}

	// Capture stdout/stderr up to MaxBufferSize each.
	stdoutBuf := &cappedBuffer{cap: MaxBufferSize}
	stderrBuf := &cappedBuffer{cap: MaxBufferSize}
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	// Configure process group so cancellation kills grandchildren.
	// Implementation in runner_unix.go / runner_windows.go (build tags).
	applyProcAttr(cmd)

	start := time.Now()
	logger.Debug("shellrun.start", "command", command, "cwd", opt.cwd)

	startErr := cmd.Start()
	if startErr != nil {
		logger.Error("shellrun.start_failed", "command", command, "error", startErr)
		return &Result{
			Command:  command,
			ExitCode: 1, // OPS_USAGE
			Duration: time.Since(start),
		}, fmt.Errorf("shellrun: start %q: %w", name, startErr)
	}

	// Wait with cancellation-aware kill propagation.
	waitErr := waitWithCancellation(ctx, cmd, r.GracePeriod, logger, command)
	duration := time.Since(start)

	result := &Result{
		Command:   command,
		Stdout:    stdoutBuf.String(),
		Stderr:    stderrBuf.String(),
		Duration:  duration,
		Truncated: stdoutBuf.truncated || stderrBuf.truncated,
	}

	// Derive exit code.
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	} else if waitErr != nil {
		// Process never exited cleanly (e.g., killed before Start completed).
		result.ExitCode = -1
	}

	logFields := []any{
		"command", command,
		"exit_code", result.ExitCode,
		"duration_ms", duration.Milliseconds(),
	}
	if result.Truncated {
		logFields = append(logFields, "truncated", true)
	}

	if waitErr != nil {
		logger.Warn("shellrun.exit_nonzero", logFields...)
		return result, waitErr
	}
	logger.Debug("shellrun.exit_ok", logFields...)
	return result, nil
}

// waitWithCancellation waits for the subprocess to exit. If the context
// is cancelled, sends SIGTERM to the process group, waits GracePeriod,
// then SIGKILL. Returns the underlying wait error (nil on clean exit).
func waitWithCancellation(ctx context.Context, cmd *exec.Cmd, grace time.Duration, logger *slog.Logger, command string) error {
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		// Context cancelled — kill the process group.
		logger.Warn("shellrun.context_cancelled", "command", command, "cause", ctx.Err())
		if err := killProcessGroup(cmd, false); err != nil {
			logger.Warn("shellrun.sigterm_failed", "error", err)
		}

		// Wait for grace period OR clean exit.
		select {
		case err := <-done:
			return err
		case <-time.After(grace):
			logger.Warn("shellrun.grace_expired_sigkill", "command", command, "grace_ms", grace.Milliseconds())
			if err := killProcessGroup(cmd, true); err != nil {
				logger.Warn("shellrun.sigkill_failed", "error", err)
			}
			return <-done
		}
	}
}

// cappedBuffer is an io.Writer that drops bytes after `cap` bytes are
// captured, setting truncated=true. Prevents OOM on pathological output.
type cappedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	cap       int
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	remaining := c.cap - c.buf.Len()
	if remaining <= 0 {
		c.truncated = true
		return len(p), nil // pretend we wrote, drop on floor
	}
	if len(p) <= remaining {
		return c.buf.Write(p)
	}
	// Write only what fits; mark truncated.
	n, _ := c.buf.Write(p[:remaining])
	c.truncated = true
	return n + (len(p) - remaining), nil // pretend we accepted all
}

func (c *cappedBuffer) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// Static check that DefaultRunner satisfies Runner.
var _ Runner = (*DefaultRunner)(nil)
