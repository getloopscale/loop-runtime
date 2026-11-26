// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package aggregator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"

	"github.com/getloopscale/loop-runtime/api/internal/timeline"
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
// Returns ports.ErrDeployNotFound when the deploy is unknown; any other
// lookup failure is returned as such.
func (s *PostgresDeployTimelineStore) DeployTenantID(ctx context.Context, deployID string) (string, error) {
	return s.ownerOf(ctx, deployID)
}

// GetAfterForDeploy returns timeline events for the (tenant, deploy)
// pair. The handler-side verifies the tenant matches via DeployTenantID
// BEFORE invoking this method, so the tenant arg is the resolved-owner
// — this method honors ADR D8 by re-validating ownership at the read
// boundary (defense-in-depth).
func (s *PostgresDeployTimelineStore) GetAfterForDeploy(ctx context.Context, tenantID, deployID string, afterID int64) ([]timeline.DeployTimelineEvent, error) {
	// Defense-in-depth re-check (ADR D8).
	owner, err := s.ownerOf(ctx, deployID)
	if err != nil {
		return nil, err
	}
	if owner != tenantID {
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

// ownerOf resolves the tenant owning deployID. A missing build is
// ports.ErrDeployNotFound; any other lookup failure is returned as itself,
// so an outage answers 500 instead of reading as "not found".
func (s *PostgresDeployTimelineStore) ownerOf(ctx context.Context, deployID string) (string, error) {
	tenant, err := s.BuildLookup.GetTenantForBuild(ctx, deployID)
	if errors.Is(err, loopzeroerrors.ErrBuildNotFound) || (err == nil && tenant == "") {
		return "", timeline.ErrDeployNotFound
	}
	if err != nil {
		return "", fmt.Errorf("deploy timeline: resolve owner of %s: %w", deployID, err)
	}
	return tenant, nil
}
