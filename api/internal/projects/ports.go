// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package projects is the project feature: creating, listing and deleting a
// tenant's projects. Its handlers live in projectshttp.
package projects

import "context"

// ProjectDeleteInput is the input for deleting a project.
type ProjectDeleteInput struct {
	TenantID      string
	ProjectID     string
	ProjectName   string
	Environment   string // empty = all environments
	DeletedBy     string
	CorrelationID string
}

// ProjectDeleteOutput is the result of project deletion.
type ProjectDeleteOutput struct {
	ProjectName         string   `json:"project"`
	DeletedEnvironments []string `json:"deleted_environments"`
	CancelledBuilds     int      `json:"cancelled_builds"`
}

// Deleter abstracts project deletion orchestration (DIP). ProjectDeleter is the
// production implementation; the handlers depend on this port only.
type Deleter interface {
	Delete(ctx context.Context, req ProjectDeleteInput) (*ProjectDeleteOutput, error)
}
