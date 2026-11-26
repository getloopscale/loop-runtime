// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package cleanroom implements T3.1 of plan canonical-tests-rewrite-v2 v1.0
// per ADR-2026-06-17-T3 D1 (Ginkgo BDD + internal/livetest/ helpers).
//
// Wraps the 6 canonical fases that previously lived in the v1 bash script
// `archive/canonical-tests-v1/scripts/local/cleanroom-canonical-chain-runner.sh`:
//
//	Phase 0 — Baseline pre-rebuild capture
//	Phase 1 — task clean:all (destroy 4 k3d clusters)
//	Phase 2 — task setup (recreate infra)
//	Phase 3 — tilt up (dev loop in background)
//	Phase 4 — task doctor (gate PASS=38+ FAIL=0)
//	Phase 5 — Baseline post-rebuild diff
//
// Each phase is a function in this package that exec's the underlying tooling.
// Tests live in cleanroom_test.go with Ginkgo BDD layout.
//
// The functional contract per cycle-implement.md:
//   - Idempotent (Phase 2 setup runnable multiple times)
//   - Deterministic (baseline diff: same fixture → same verdict)
//   - Honest (failures propagate cleanly; no silent retries)
//   - Observable (each fase emits a structured log line + OTel span when wired)
package cleanroom

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Options configures the cleanroom orchestrator's behavior. Mirrors the v1
// script's flags: --skip-cleanroom, --skip-sponsor, --skip-baseline.
type Options struct {
	RepoRoot      string        // absolute path to loopruntime repo root
	SkipBaseline  bool          // skip Phase 0 + Phase 5 (back-compat escape hatch)
	SkipCleanroom bool          // skip Phases 1-3 (when env is already up; debug iteration)
	SkipDoctor    bool          // skip Phase 4 doctor gate (NOT recommended)
	TaskCmd       string        // path to task binary (default: "task" via PATH)
	TiltCmd       string        // path to tilt binary (default: "tilt" via PATH)
	BaselineCmd   string        // path to baseline-capture.sh (default: infra/scripts/local/baseline-capture.sh)
	BaselineDiff  string        // path to baseline-diff.sh (default: infra/scripts/local/baseline-diff.sh)
	DoctorTimeout time.Duration // max time to wait for task doctor PASS=38+ FAIL=0 (default 10min)
	TiltBootWait  time.Duration // time to wait after tilt up before Phase 4 (default 240s — per T6.2 fix)

	// Streams for human-readable progress. If nil, fall back to os.Stdout / os.Stderr.
	Out io.Writer
	Err io.Writer
}

// DestroyConsentEnv names the explicit consent a spec needs before it deletes and
// recreates the k3d clusters. A spec that destroys infrastructure must not run by
// accident, and it must not refuse unconditionally either: that was #393, where
// every spec skipped and `task cleanroom:v2` reported SUCCESS having run nothing.
const DestroyConsentEnv = "LOOPRUNTIME_CLEANROOM_DESTROY"

// DestroyConsented reports whether the caller granted DestroyConsentEnv. Only the
// exact value "1" counts, so a stray "false" or "0" can never read as consent.
func DestroyConsented() bool {
	return os.Getenv(DestroyConsentEnv) == "1"
}

// DefaultOptions returns canonical defaults — matching the v1 chain runner.
func DefaultOptions(repoRoot string) Options {
	return Options{
		RepoRoot:      repoRoot,
		TaskCmd:       "task",
		TiltCmd:       "tilt",
		BaselineCmd:   filepath.Join("infra", "scripts", "local", "baseline-capture.sh"),
		BaselineDiff:  filepath.Join("infra", "scripts", "local", "baseline-diff.sh"),
		DoctorTimeout: 10 * time.Minute,
		TiltBootWait:  240 * time.Second,
		Out:           os.Stdout,
		Err:           os.Stderr,
	}
}

// PhaseResult captures the outcome of one phase execution.
type PhaseResult struct {
	Phase    string
	OK       bool
	Detail   string
	Duration time.Duration
}

// Outcome aggregates all phase results plus a roll-up verdict.
type Outcome struct {
	Phases  []PhaseResult
	OK      bool
	Started time.Time
	Ended   time.Time
}

// Run executes the full chain in order. Stops at the first failed phase
// unless SkipDoctor / SkipBaseline flags say otherwise (matching v1 semantics).
//
// Caller is responsible for context cancellation (e.g., test timeout).
func Run(ctx context.Context, opts Options) (*Outcome, error) {
	if opts.RepoRoot == "" {
		return nil, fmt.Errorf("RepoRoot is required")
	}
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	if opts.Err == nil {
		opts.Err = os.Stderr
	}

	outcome := &Outcome{Started: time.Now(), OK: true}

	// Phase 0
	if !opts.SkipBaseline && !opts.SkipCleanroom {
		r := runPhase(ctx, "Phase 0 — baseline pre-rebuild", func() error {
			return BaselineCapture(ctx, opts, "/tmp/baseline-pre-rebuild.json")
		})
		outcome.Phases = append(outcome.Phases, r)
		if !r.OK {
			outcome.OK = false
			outcome.Ended = time.Now()
			return outcome, nil
		}
	}

	if !opts.SkipCleanroom {
		// Phase 1
		r := runPhase(ctx, "Phase 1 — task clean:all", func() error {
			return TaskRun(ctx, opts, "clean:all")
		})
		outcome.Phases = append(outcome.Phases, r)
		if !r.OK {
			outcome.OK = false
			outcome.Ended = time.Now()
			return outcome, nil
		}

		// Phase 2
		r = runPhase(ctx, "Phase 2 — task setup", func() error {
			return TaskRun(ctx, opts, "setup")
		})
		outcome.Phases = append(outcome.Phases, r)
		if !r.OK {
			outcome.OK = false
			outcome.Ended = time.Now()
			return outcome, nil
		}

		// Phase 3
		r = runPhase(ctx, "Phase 3 — tilt up + boot wait", func() error {
			return TiltUpBackground(ctx, opts)
		})
		outcome.Phases = append(outcome.Phases, r)
		if !r.OK {
			outcome.OK = false
			outcome.Ended = time.Now()
			return outcome, nil
		}
	}

	// Phase 4
	if !opts.SkipDoctor {
		r := runPhase(ctx, "Phase 4 — task doctor PASS=38+ FAIL=0", func() error {
			return TaskDoctorAssertReady(ctx, opts)
		})
		outcome.Phases = append(outcome.Phases, r)
		if !r.OK {
			outcome.OK = false
			outcome.Ended = time.Now()
			return outcome, nil
		}
	}

	// Phase 5
	if !opts.SkipBaseline && !opts.SkipCleanroom {
		r := runPhase(ctx, "Phase 5 — baseline post-rebuild + diff", func() error {
			if err := BaselineCapture(ctx, opts, "/tmp/baseline-post-rebuild.json"); err != nil {
				return err
			}
			return BaselineDiff(ctx, opts, "/tmp/baseline-pre-rebuild.json", "/tmp/baseline-post-rebuild.json")
		})
		outcome.Phases = append(outcome.Phases, r)
		if !r.OK {
			outcome.OK = false
		}
	}

	outcome.Ended = time.Now()
	return outcome, nil
}

func runPhase(ctx context.Context, label string, fn func() error) PhaseResult {
	start := time.Now()
	err := fn()
	pr := PhaseResult{
		Phase:    label,
		Duration: time.Since(start),
		OK:       err == nil,
	}
	if err != nil {
		pr.Detail = err.Error()
	}
	return pr
}

// TaskRun executes `task <args...>` from RepoRoot.
func TaskRun(ctx context.Context, opts Options, args ...string) error {
	cmd := exec.CommandContext(ctx, opts.TaskCmd, args...)
	cmd.Dir = opts.RepoRoot
	cmd.Stdout = opts.Out
	cmd.Stderr = opts.Err
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("task %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// TiltContext is the kube context the dev loop runs against. Tilt deploys into the
// CURRENT context unless told otherwise, and a cleanroom leaves whichever context
// was current before it — on the droplet that was the secrets cluster, so the
// platform went there (#402).
const TiltContext = "k3d-loopruntime-core"

// tiltUpArgs pins the context rather than inheriting it.
func tiltUpArgs() []string {
	return []string{"up", "--context", TiltContext}
}

// TiltUpBackground spawns `tilt up` detached, then sleeps TiltBootWait.
// Returns nil if tilt PID stays alive through the wait window.
func TiltUpBackground(ctx context.Context, opts Options) error {
	// Kill any stale tilt instance per j17-aprendizado-tilt-stale-instance pattern
	pkill := exec.CommandContext(ctx, "pkill", "-f", "tilt up")
	_ = pkill.Run() // ignore error: ok if no tilt was running

	tiltLog, err := os.OpenFile("/tmp/cleanroom-tilt.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("open tilt log: %w", err)
	}
	defer tiltLog.Close()

	cmd := exec.CommandContext(ctx, opts.TiltCmd, tiltUpArgs()...)
	cmd.Dir = opts.RepoRoot
	cmd.Stdout = tiltLog
	cmd.Stderr = tiltLog
	cmd.SysProcAttr = sysProcAttrDetached()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("tilt up start: %w", err)
	}

	// Sleep up to TiltBootWait, honoring context cancellation
	select {
	case <-time.After(opts.TiltBootWait):
	case <-ctx.Done():
		return ctx.Err()
	}

	// Verify tilt still alive
	if cmd.Process == nil {
		return fmt.Errorf("tilt process gone before boot wait completed (see /tmp/cleanroom-tilt.log)")
	}
	return nil
}

// TaskDoctorAssertReady runs `task doctor` and parses output for "FAIL: 0".
// Retries on transient failure (e.g., pod still booting) until DoctorTimeout.
func TaskDoctorAssertReady(ctx context.Context, opts Options) error {
	deadline := time.Now().Add(opts.DoctorTimeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		cmd := exec.CommandContext(ctx, opts.TaskCmd, "doctor")
		cmd.Dir = opts.RepoRoot
		out, _ := cmd.CombinedOutput()
		// Match "FAIL: 0" on a line (case-insensitive variants OK).
		if strings.Contains(string(out), "FAIL: 0") && !strings.Contains(string(out), "FAIL: 0\nFAIL:") {
			fmt.Fprintln(opts.Out, "task doctor: PASS=38+ FAIL=0")
			return nil
		}
		// Brief sleep before retry
		select {
		case <-time.After(15 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return fmt.Errorf("task doctor did not reach FAIL=0 within %v", opts.DoctorTimeout)
}

// BaselineCapture runs the canonical baseline-capture.sh into the given output JSON.
// The bash helper IS NOT moved by T0.1 (it's not part of the v1 quarantine; it
// supports both v1 and v2 chains).
func BaselineCapture(ctx context.Context, opts Options, outputJSON string) error {
	cmd := exec.CommandContext(ctx, "bash", opts.BaselineCmd, "--output", outputJSON)
	cmd.Dir = opts.RepoRoot
	cmd.Stdout = opts.Out
	cmd.Stderr = opts.Err
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("baseline-capture %s: %w", outputJSON, err)
	}
	return nil
}

// BaselineDiff runs baseline-diff.sh and returns error if verdict is FAIL (exit 1).
func BaselineDiff(ctx context.Context, opts Options, pre, post string) error {
	cmd := exec.CommandContext(ctx, "bash", opts.BaselineDiff, pre, post)
	cmd.Dir = opts.RepoRoot
	cmd.Stdout = opts.Out
	cmd.Stderr = opts.Err
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("baseline-diff %s vs %s: %w", pre, post, err)
	}
	return nil
}
