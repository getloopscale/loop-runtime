// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package db

import "time"

// The states of a rollback. It starts IN_PROGRESS and is closed once.
const (
	RollbackInProgress = "IN_PROGRESS"
	RollbackSucceeded  = "SUCCEEDED"
	RollbackFailed     = "FAILED"
)

// Rollback is the record of one rollback of an environment: who asked for it,
// the release it left and the release it aimed at, and how it ended. The
// release and build ids are copies; the record outlives the releases.
type Rollback struct {
	ID            string
	TenantID      string
	ProjectID     string
	Environment   string
	FromReleaseID string
	FromBuildID   string
	ToReleaseID   string
	ToBuildID     string
	Status        string
	// FailureReason is why a FAILED rollback left the environment as it was.
	FailureReason string
	CreatedBy     string
	CorrelationID string
	CreatedAt     time.Time
	// FinishedAt is nil while the rollback is in progress.
	FinishedAt *time.Time
}
