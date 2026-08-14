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
