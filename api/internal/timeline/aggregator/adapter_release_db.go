// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package aggregator

import (
	"context"
	"fmt"
	"time"

	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// ReleaseStatusRow is the source-side projection of api/internal/db
// releases for the deploy_timeline aggregator. Decoupled from the db
// package so this adapter remains hermetic-testable without sqlmock.
type ReleaseStatusRow struct {
	State     string // matches DTStatus + DEGRADED in deploy phase
	UpdatedAt time.Time
	Reason    string
}

// ReleaseStatusQuerier is the I/O contract — implementations poll
// PostgreSQL releases table for status transitions. Wire-up: T5.1.
type ReleaseStatusQuerier interface {
	QueryReleaseStatus(ctx context.Context, deployID string) (*ReleaseStatusRow, error)
}

// ReleaseDBReader implements SourceReader for the release_db source.
// Polls every PollInterval until a terminal release status (ACTIVE,
// FAILED, ROLLED_BACK) is observed OR ctx.Done() fires.
type ReleaseDBReader struct {
	Querier      ReleaseStatusQuerier
	PollInterval time.Duration
}

// NewReleaseDBReader returns a configured reader. Defaults: 2s poll.
func NewReleaseDBReader(q ReleaseStatusQuerier) *ReleaseDBReader {
	return &ReleaseDBReader{Querier: q, PollInterval: 2 * time.Second}
}

// ID identifies the source.
func (r *ReleaseDBReader) ID() SourceID { return SourceReleaseDB }

// Stream polls release state until terminal or context cancel.
func (r *ReleaseDBReader) Stream(ctx context.Context, deployID string, out chan<- PhaseEvent) error {
	if r.PollInterval <= 0 {
		r.PollInterval = 2 * time.Second
	}

	emit := func() (bool, error) {
		row, err := r.Querier.QueryReleaseStatus(ctx, deployID)
		if err != nil {
			return false, fmt.Errorf("release_db query: %w", err)
		}
		if row == nil {
			return false, nil
		}
		phase, status, terminal := releaseStateProjection(row.State)
		reason := row.Reason
		if _, known := lookupStateProjection(row.State); !known && reason == "" {
			reason = fmt.Sprintf("unrecognised release state %q", row.State)
		}
		ev := PhaseEvent{
			Phase:          phase,
			Status:         status,
			DegradedReason: reason,
			Timestamp:      row.UpdatedAt,
		}
		select {
		case out <- ev:
		case <-ctx.Done():
			return false, ctx.Err()
		}
		return terminal, nil
	}

	return pollUntilTerminal(ctx, r.PollInterval, emit)
}

// stateProjection is where one row state lands on the deploy timeline.
type stateProjection struct {
	phase, status string
	terminal      bool
}

// ladderProjections are the synthetic states the deploy-timeline spawn site
// (api/cmd/deploy_timeline_spawn.go) emits to walk the 5-phase ladder in
// order, plus the build-phase failures it emits on build.failed and
// build.cancelled — phase=build, because the failure is upstream of release.
var ladderProjections = map[string]stateProjection{
	"BUILD_IN_PROGRESS":  {"build", "in_progress", false},
	"BUILD_COMPLETED":    {"build", "completed", false},
	"SIGN_IN_PROGRESS":   {"sign", "in_progress", false},
	"SIGN_COMPLETED":     {"sign", "completed", false},
	"DEPLOY_IN_PROGRESS": {"deploy", "in_progress", false},
	"DEPLOY_COMPLETED":   {"deploy", "completed", false},
	"LIVE_IN_PROGRESS":   {"live", "in_progress", false},
	"LIVE_COMPLETED":     {"live", "completed", true},
	"BUILD_FAILED":       {"build", "failed", true},
	"BUILD_CANCELLED":    {"build", "canceled", true},
	// The sync of the release failed: the deploy phase ends as failed, with
	// the reason of the failure on the row.
	"DEPLOY_FAILED": {"deploy", "failed", true},
}

// releaseProjections covers every state of the release state machine, keyed
// by its constant so a renamed or added state cannot silently fall through.
// A failure the machine can retry (mirror, sync) is degraded and keeps the
// stream open; one that needs a person (drift alert, failed rollback) or
// cannot move again is terminal.
var releaseProjections = map[statemachine.State]stateProjection{
	statemachine.ReleaseReady:           {"release", "in_progress", false},
	statemachine.ReleasePending:         {"release", "in_progress", false},
	statemachine.ReleaseActive:          {"release", "completed", false},
	statemachine.ReleaseGitMirrorFailed: {"release", "degraded", false},
	statemachine.ReleaseSyncFailed:      {"release", "degraded", false},
	statemachine.ReleaseDriftAlert:      {"release", "failed", true},
	statemachine.ReleaseSuperseded:      {"release", "completed", false},
	statemachine.ReleaseRollingBack:     {"release", "degraded", false},
	statemachine.ReleaseRolledBack:      {"release", "failed", true},
	statemachine.ReleaseRollbackFailed:  {"release", "failed", true},
	statemachine.ReleaseCancelled:       {"release", "canceled", true},
	statemachine.ReleaseArchived:        {"release", "completed", true},
}

// lookupStateProjection reports whether state is one this reader knows.
func lookupStateProjection(state string) (stateProjection, bool) {
	if p, ok := ladderProjections[state]; ok {
		return p, true
	}
	p, ok := releaseProjections[statemachine.State(state)]
	return p, ok
}

// releaseStateProjection maps a row state onto the timeline. An unknown state
// is shown as degraded — never as progress — so it is visible, and the stream
// stays open for the next state.
func releaseStateProjection(state string) (phase, status string, terminal bool) {
	p, ok := lookupStateProjection(state)
	if !ok {
		return "release", "degraded", false
	}
	return p.phase, p.status, p.terminal
}

// TimelineEventType names the event a state is written as:
// "deploy_timeline.<phase>.<status>". It is how a reader tells which states a
// stream already holds.
func TimelineEventType(state string) string {
	phase, status, _ := releaseStateProjection(state)
	return "deploy_timeline." + phase + "." + status
}
