// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package server_test

import (
	"context"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

type fakeProjectStore struct {
	projects  map[string]bool
	createErr error
	listErr   error
}

func (f *fakeProjectStore) CreateProject(ctx context.Context, p ports.CreateProjectInput) (ports.ProjectResult, error) {
	if f.createErr != nil {
		return ports.ProjectResult{}, f.createErr
	}
	key := p.TenantID + "/" + p.Name
	if f.projects[key] {
		return ports.ProjectResult{}, ports.ErrProjectExists
	}
	f.projects[key] = true
	return ports.ProjectResult{
		ID:     "proj-12345678",
		Name:   p.Name,
		TeamID: p.TenantID,
	}, nil
}

func (f *fakeProjectStore) GetByName(_ context.Context, tenantID, name string) (*ports.ProjectResult, error) {
	key := tenantID + "/" + name
	if f.projects[key] {
		return &ports.ProjectResult{ID: "proj-12345678", Name: name, TeamID: tenantID}, nil
	}
	return nil, nil
}

func (f *fakeProjectStore) ListByTenant(_ context.Context, tenantID string) ([]ports.ProjectResult, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var results []ports.ProjectResult
	for key := range f.projects {
		parts := splitKey(key)
		if parts[0] == tenantID {
			results = append(results, ports.ProjectResult{
				ID:     "proj-12345678",
				Name:   parts[1],
				TeamID: tenantID,
			})
		}
	}
	if results == nil {
		results = []ports.ProjectResult{}
	}
	return results, nil
}

func splitKey(key string) [2]string {
	for i, c := range key {
		if c == '/' {
			return [2]string{key[:i], key[i+1:]}
		}
	}
	return [2]string{key, ""}
}

// T2.1 + ADR D2: per-project summary variant. Mirrors ListByTenant
// shape; summary fields default to zero (post-handler-test the production
// route renders these in the response — these unit tests focus on the
// shape/auth/error paths, not the summary semantics, which live in
// status_mapper_test.go + integration tests).
func (f *fakeProjectStore) ListByTenantWithSummary(_ context.Context, tenantID string) ([]ports.ProjectSummary, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var results []ports.ProjectSummary
	for key := range f.projects {
		parts := splitKey(key)
		if parts[0] == tenantID {
			results = append(results, ports.ProjectSummary{
				ID:               "proj-12345678",
				Name:             parts[1],
				TeamID:           tenantID,
				EnvironmentCount: 0,
				LastDeployStatus: "idle",
			})
		}
	}
	if results == nil {
		results = []ports.ProjectSummary{}
	}
	return results, nil
}

func (f *fakeProjectStore) DeleteProject(_ context.Context, _, _ string) error {
	return nil
}

type fakeTenantCRManager struct {
	calls    int
	lastSpec ports.TenantCRSpec
	err      error
}

func (f *fakeTenantCRManager) EnsureTenant(ctx context.Context, tenantID string, spec ports.TenantCRSpec) error {
	f.calls++
	f.lastSpec = spec
	return f.err
}
