// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package aggregator

import (
	"context"
	"fmt"
	"time"
)

// ReleaseStatusRow is the source-side projection of api/internal/db
// releases for the deploy_timeline aggregator. Decoupled from the db
// package so this adapter remains hermetic-testable without sqlmock.
type ReleaseStatusRow struct {
	State     string    // matches DTStatus + DEGRADED in deploy phase
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
	ticker := time.NewTicker(r.PollInterval)
	defer ticker.Stop()

	emit := func() (bool, error) {
		row, err := r.Querier.QueryReleaseStatus(ctx, deployID)
		if err != nil {
			return false, fmt.Errorf("release_db query: %w", err)
		}
		if row == nil {
			return false, nil
		}
		// Release state → (phase, status) projection:
		//   PENDING  → release / in_progress
		//   ACTIVE   → release / completed (then deploy phase begins)
		//   ROLLING_BACK → release / degraded (non-terminal in this source)
		//   FAILED, ROLLED_BACK → release / failed
		phase, status, terminal := releaseStateProjection(row.State)
		ev := PhaseEvent{
			Phase:          phase,
			Status:         status,
			DegradedReason: row.Reason,
			Timestamp:      row.UpdatedAt,
		}
		select {
		case out <- ev:
		case <-ctx.Done():
			return false, ctx.Err()
		}
		return terminal, nil
	}

	if term, err := emit(); err != nil {
		return err
	} else if term {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			term, err := emit()
			if err != nil {
				return err
			}
			if term {
				return nil
			}
		}
	}
}

func releaseStateProjection(state string) (phase, status string, terminal bool) {
	switch state {
	case "PENDING", "READY":
		return "release", "in_progress", false
	case "ACTIVE":
		return "release", "completed", false
	case "ROLLING_BACK":
		return "release", "degraded", false
	case "FAILED":
		return "release", "failed", true
	case "ROLLED_BACK":
		return "release", "failed", true
	case "SUPERSEDED":
		return "release", "completed", false
	case "CANCELLED":
		return "release", "canceled", true
	}
	return "release", "in_progress", false
}
