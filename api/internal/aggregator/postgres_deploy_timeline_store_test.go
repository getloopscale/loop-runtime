// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package aggregator

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"

	"github.com/getloopscale/loopzero/api/internal/ports"
)

type fakeBuildLookup struct {
	tenant string
	err    error
}

func (f *fakeBuildLookup) GetTenantForBuild(ctx context.Context, buildID string) (string, error) {
	return f.tenant, f.err
}

type fakeReader struct {
	rows []SSEEventRow
}

func (f *fakeReader) GetAfter(ctx context.Context, buildID string, afterID int64) ([]SSEEventRow, error) {
	out := make([]SSEEventRow, 0)
	for _, r := range f.rows {
		if r.ID > afterID {
			out = append(out, r)
		}
	}
	return out, nil
}

// TestPostgresStore_DeployTenantID_ReturnsOwner happy path.
func TestPostgresStore_DeployTenantID_ReturnsOwner(t *testing.T) {
	s := NewPostgresDeployTimelineStore(&fakeBuildLookup{tenant: "tn-x"}, &fakeReader{})
	got, err := s.DeployTenantID(context.Background(), "dep-1")
	if err != nil {
		t.Fatalf("DeployTenantID err = %v", err)
	}
	if got != "tn-x" {
		t.Errorf("tenant = %q; want tn-x", got)
	}
}

// TestPostgresStore_DeployTenantID_UnknownReturnsNotFound confirms the
// existence-leak guard maps lookup-failure and empty-tenant to the same
// canonical ErrDeployNotFound.
func TestPostgresStore_DeployTenantID_UnknownReturnsNotFound(t *testing.T) {
	tests := []struct {
		name string
		l    *fakeBuildLookup
	}{
		{"build not found", &fakeBuildLookup{err: loopzeroerrors.ErrBuildNotFound}},
		{"empty tenant", &fakeBuildLookup{tenant: ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewPostgresDeployTimelineStore(tt.l, &fakeReader{})
			_, err := s.DeployTenantID(context.Background(), "dep-x")
			if !errors.Is(err, ports.ErrDeployNotFound) {
				t.Errorf("expected ErrDeployNotFound; got %v", err)
			}
		})
	}
}

// TestPostgresStore_GetAfterForDeploy_TenantMismatch confirms ADR D8
// defense-in-depth: even with valid tenant + valid deploy, mismatched
// pair returns ErrDeployNotFound.
func TestPostgresStore_GetAfterForDeploy_TenantMismatch(t *testing.T) {
	s := NewPostgresDeployTimelineStore(
		&fakeBuildLookup{tenant: "tn-owner"},
		&fakeReader{rows: []SSEEventRow{{ID: 1, EventType: "x", Data: json.RawMessage(`{}`)}}},
	)
	_, err := s.GetAfterForDeploy(context.Background(), "tn-attacker", "dep-1", 0)
	if !errors.Is(err, ports.ErrDeployNotFound) {
		t.Errorf("expected ErrDeployNotFound; got %v", err)
	}
}

// TestPostgresStore_GetAfterForDeploy_TenantMatchReturnsRows happy path.
func TestPostgresStore_GetAfterForDeploy_TenantMatchReturnsRows(t *testing.T) {
	s := NewPostgresDeployTimelineStore(
		&fakeBuildLookup{tenant: "tn-owner"},
		&fakeReader{rows: []SSEEventRow{
			{ID: 1, EventType: "deploy_timeline.build.in_progress", Data: json.RawMessage(`{}`)},
			{ID: 2, EventType: "deploy_timeline.build.completed", Data: json.RawMessage(`{}`)},
		}},
	)
	got, err := s.GetAfterForDeploy(context.Background(), "tn-owner", "dep-1", 1)
	if err != nil {
		t.Fatalf("GetAfterForDeploy err = %v", err)
	}
	if len(got) != 1 || got[0].ID != 2 {
		t.Errorf("got %d events; first.ID = %d", len(got), func() int64 {
			if len(got) > 0 {
				return got[0].ID
			}
			return 0
		}())
	}
}

// An outage is not a missing deploy: a lookup that fails for any other
// reason must surface as that failure (the handler answers 500), not as
// "not found".
func TestPostgresStore_DeployTenantID_LookupOutageIsNotNotFound(t *testing.T) {
	outage := errors.New("connection refused")
	s := NewPostgresDeployTimelineStore(&fakeBuildLookup{err: outage}, &fakeReader{})

	_, err := s.DeployTenantID(context.Background(), "dep-x")

	if errors.Is(err, ports.ErrDeployNotFound) || !errors.Is(err, outage) {
		t.Fatalf("err = %v, want the outage, not ErrDeployNotFound", err)
	}
}
