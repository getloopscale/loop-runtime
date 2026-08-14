// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Test fixtures the parent package still needs after the projects and
// environments families were extracted (B-080, slice-36).
//
// Duplicated from those packages rather than exported from them: DRY governs
// knowledge, not lines, and importing a family's test fixture from the parent
// would be the parent→family edge the routes fence forbids — in the direction
// that looks harmless right up until the parent can no longer be emptied.
package routes

import (
	"context"

	"github.com/getloopscale/loopzero/api/internal/domain"
)

type fakeProjectStore struct {
	projects       []domain.ProjectResult
	projectSummary []domain.ProjectSummary // T2.1: per-project summary fixture
	project        *domain.ProjectResult
	createResult   domain.ProjectResult
	createErr      error
	deleteErr      error
	summaryErr     error
}

func (f *fakeProjectStore) CreateProject(_ context.Context, input domain.CreateProjectInput) (domain.ProjectResult, error) {
	if f.createErr != nil {
		return domain.ProjectResult{}, f.createErr
	}
	return f.createResult, nil
}

func (f *fakeProjectStore) GetByName(_ context.Context, _, name string) (*domain.ProjectResult, error) {
	if f.project != nil && f.project.Name == name {
		return f.project, nil
	}
	return nil, nil
}

func (f *fakeProjectStore) ListByTenant(_ context.Context, _ string) ([]domain.ProjectResult, error) {
	return f.projects, nil
}

// T2.1 + ADR D2: per-project summary variant. If the test sets
// f.projectSummary it wins (lets a test pin specific summary fields).
// Otherwise, auto-promote f.projects (legacy fixture field) to
// summaries with zero env_count + idle status — preserves backward
// compat for the ~12 tests that wrote f.projects directly.
func (f *fakeProjectStore) ListByTenantWithSummary(_ context.Context, _ string) ([]domain.ProjectSummary, error) {
	if f.summaryErr != nil {
		return nil, f.summaryErr
	}
	if f.projectSummary != nil {
		return f.projectSummary, nil
	}
	out := make([]domain.ProjectSummary, 0, len(f.projects))
	for _, p := range f.projects {
		out = append(out, domain.ProjectSummary{
			ID:               p.ID,
			Name:             p.Name,
			TeamID:           p.TeamID,
			CreatedBy:        p.CreatedBy,
			CreatedAt:        p.CreatedAt,
			EnvironmentCount: 0,
			LastDeployStatus: "idle",
		})
	}
	return out, nil
}

func (f *fakeProjectStore) DeleteProject(_ context.Context, _, _ string) error {
	return f.deleteErr
}

type fakeEnvMgr struct {
	envs      []domain.EnvironmentEntry
	detail    *domain.EnvironmentDetail
	addErr    error
	removeErr error
	listErr   error
}

func (f *fakeEnvMgr) ListEnvironments(_ context.Context, _ string) ([]domain.EnvironmentEntry, error) {
	return f.envs, f.listErr
}

// M2 v1.2: signature carries `project`. Phase 1 cascade — fake records
// the project arg but ignores it for return-value purposes (test
// fixtures land in Phase 4 when the project arg semantics matter).
func (f *fakeEnvMgr) AddEnvironment(_ context.Context, _, _, _ string) error {
	return f.addErr
}

func (f *fakeEnvMgr) RemoveEnvironment(_ context.Context, _, _, _ string) error {
	return f.removeErr
}

func (f *fakeEnvMgr) GetEnvironmentDetail(_ context.Context, _ string, _ string, envName string) (*domain.EnvironmentDetail, error) {
	if f.detail != nil && f.detail.Name == envName {
		return f.detail, nil
	}
	return nil, nil
}
