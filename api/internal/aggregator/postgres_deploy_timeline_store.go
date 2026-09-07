// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package aggregator

import (
	"context"
	"encoding/json"

	"github.com/getloopscale/loopzero/api/internal/routes/timeline"
)

// SSEReader is the minimal read surface from sse.EventStore the deploy
// timeline reader uses. Kept narrow to avoid importing sse (cyclic).
type SSEReader interface {
	GetAfter(ctx context.Context, buildID string, afterID int64) ([]SSEEventRow, error)
}

// SSEEventRow mirrors sse.SSEEvent for the read path without importing
// the sse package.
type SSEEventRow struct {
	ID        int64
	EventType string
	Data      json.RawMessage
	CreatedAt int64 // unix nanos — frontend converts
}

// BuildLookup returns the canonical tenant_id for a build/deploy ID.
// Implemented by api/internal/db.BuildStore via the sse.BuildInfo path.
type BuildLookup interface {
	GetTenantForBuild(ctx context.Context, buildID string) (string, error)
}

// PostgresDeployTimelineStore composes a build-lookup adapter and an
// SSE event reader into the routes.DeployTimelineEventStore contract.
//
// Plan T5.1: the canonical production adapter. Wired in setup_server.go
// from existing BuildStoreReader + PostgresEventStore.
type PostgresDeployTimelineStore struct {
	BuildLookup BuildLookup
	Reader      SSEReader
}

// NewPostgresDeployTimelineStore constructs the adapter.
func NewPostgresDeployTimelineStore(b BuildLookup, r SSEReader) *PostgresDeployTimelineStore {
	return &PostgresDeployTimelineStore{BuildLookup: b, Reader: r}
}

// DeployTenantID returns the tenant_id owning the given deploy_id.
// Returns routes.ErrDeployNotFound when the deploy is unknown OR the
// build-lookup errors (defense vs existence leak).
func (s *PostgresDeployTimelineStore) DeployTenantID(ctx context.Context, deployID string) (string, error) {
	tenant, err := s.BuildLookup.GetTenantForBuild(ctx, deployID)
	if err != nil || tenant == "" {
		return "", timeline.ErrDeployNotFound
	}
	return tenant, nil
}

// GetAfterForDeploy returns timeline events for the (tenant, deploy)
// pair. The handler-side verifies the tenant matches via DeployTenantID
// BEFORE invoking this method, so the tenant arg is the resolved-owner
// — this method honors ADR D8 by re-validating ownership at the read
// boundary (defense-in-depth).
func (s *PostgresDeployTimelineStore) GetAfterForDeploy(ctx context.Context, tenantID, deployID string, afterID int64) ([]timeline.DeployTimelineEvent, error) {
	// Defense-in-depth re-check (ADR D8).
	owner, err := s.BuildLookup.GetTenantForBuild(ctx, deployID)
	if err != nil || owner == "" || owner != tenantID {
		return nil, timeline.ErrDeployNotFound
	}
	rows, err := s.Reader.GetAfter(ctx, deployID, afterID)
	if err != nil {
		return nil, err
	}
	out := make([]timeline.DeployTimelineEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, timeline.DeployTimelineEvent{
			ID:        row.ID,
			EventType: row.EventType,
			Data:      row.Data,
		})
	}
	return out, nil
}
