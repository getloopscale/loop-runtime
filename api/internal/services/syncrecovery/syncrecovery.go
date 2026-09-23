// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package syncrecovery owns the one policy for driving an ArgoCD sync past a stuck
// operation: trigger the sync, and when ArgoCD answers that an operation is already
// in progress, terminate that operation before retrying.
//
// The rule used to be written four times — container delivery, the convergence
// loop and the two project-delete paths — each with its own budget, backoff and a
// duck-typed assertion for TerminateOperation that could fail silently. A change to
// the policy had to be made in four places and was not. Callers now differ only in
// the Policy they pass; the mechanism is here once.
package syncrecovery

import (
	"context"
	"errors"
	"log/slog"
	"time"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// Syncer is the part of the sync client the recovery policy drives. Both methods
// are required: a client that cannot terminate an operation cannot recover from a
// stuck one, and discovering that at runtime is what the old type assertions did.
type Syncer interface {
	TriggerSync(ctx context.Context, appName string) error
	TerminateOperation(ctx context.Context, appName string) error
}

// Policy is the retry budget of one caller.
type Policy struct {
	// MaxAttempts bounds the attempts when the latest failure is NOT a stuck
	// operation (connection refused, 5xx, auth).
	MaxAttempts int
	// MaxStuckAttempts bounds the attempts when the latest failure IS a stuck
	// operation. It is usually larger: a peer's sync may simply be finishing.
	MaxStuckAttempts int
	// TerminateAfterStuck is the number of consecutive stuck failures after which
	// the stuck operation is terminated before the next attempt.
	TerminateAfterStuck int
	// Backoff returns the wait before the next attempt, given how many attempts
	// have failed so far (1-based) and whether the latest failure was stuck.
	Backoff func(failures int, stuck bool) time.Duration
}

// ShouldTerminate reports whether consecutiveStuck stuck failures warrant
// terminating the operation. Exported for the convergence loop, which retries
// across ticks rather than inside one call and so keeps its own counter.
func (p Policy) ShouldTerminate(consecutiveStuck int) bool {
	return p.TerminateAfterStuck > 0 && consecutiveStuck >= p.TerminateAfterStuck
}

// IsStuck reports whether err is ArgoCD refusing a sync because an operation is
// already running on the application.
func IsStuck(err error) bool {
	return errors.Is(err, loopzeroerrors.ErrSyncOperationInProgress)
}

// Terminate kills the in-flight operation on appName, logs the outcome at the
// level it deserves, and reports whether the operation was terminated. A failed
// termination is not returned as an error because no caller can do more with it
// than log it: the next TriggerSync is what reports whether the lock cleared, and
// that error IS returned to the caller.
func Terminate(ctx context.Context, s Syncer, appName string, logger *slog.Logger) bool {
	if err := s.TerminateOperation(ctx, appName); err != nil {
		logger.Error("failed to terminate stuck argocd operation", "app", appName, "error", err)
		return false
	}
	logger.Info("terminated stuck argocd operation", "app", appName)
	return true
}

// Trigger runs TriggerSync on appName under policy p, terminating a stuck
// operation when the policy says so. It returns nil on the first successful
// trigger, ctx.Err() when the context ends during a backoff, and otherwise the
// last trigger error once the budget is exhausted.
func Trigger(ctx context.Context, s Syncer, appName string, p Policy, logger *slog.Logger) error {
	return trigger(ctx, s, appName, p, logger, sleep)
}

func trigger(ctx context.Context, s Syncer, appName string, p Policy, logger *slog.Logger,
	wait func(context.Context, time.Duration) error) error {
	consecutiveStuck := 0
	for failures := 1; ; failures++ {
		err := s.TriggerSync(ctx, appName)
		if err == nil {
			return nil
		}
		stuck := IsStuck(err)
		if stuck {
			consecutiveStuck++
		} else {
			consecutiveStuck = 0
		}
		budget := p.MaxAttempts
		if stuck {
			budget = p.MaxStuckAttempts
		}
		if failures >= budget {
			return err
		}
		logger.Warn("argocd sync trigger failed, retrying",
			"app", appName, "attempt", failures, "stuck_operation", stuck, "error", err)
		if stuck && p.ShouldTerminate(consecutiveStuck) {
			Terminate(ctx, s, appName, logger)
		}
		if waitErr := wait(ctx, p.Backoff(failures, stuck)); waitErr != nil {
			return waitErr
		}
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
