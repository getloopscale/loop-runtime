// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/loopzero/loopzero/api/internal/middleware"
)

type stubMembershipLookup struct {
	memberships []middleware.TenantMembership
	err         error
}

func (s *stubMembershipLookup) GetMemberships(_ context.Context, _ string) ([]middleware.TenantMembership, error) {
	return s.memberships, s.err
}

func makeWhoamiRequest(uc *middleware.UserContext) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/whoami", nil)
	if uc != nil {
		return withAuth(req, uc)
	}
	return req
}

// TestWhoami_ReturnsTeamsArrayAndActiveTeam asserts plan T2.5 D8 — the
// response includes a `teams` array AND an `active_team` field when a
// MembershipLookup is wired.
// Plan T2.5 RED #1.
func TestWhoami_ReturnsTeamsArrayAndActiveTeam(t *testing.T) {
	handler := NewAuthHandler(nil).WithMembershipLookup(&stubMembershipLookup{
		memberships: []middleware.TenantMembership{
			{TenantID: "tn-e2etest", Role: "owner"},
			{TenantID: "e2e-tenant", Role: "developer"},
		},
	})

	rec := httptest.NewRecorder()
	req := makeWhoamiRequest(&middleware.UserContext{
		UserID:   "user-1",
		TenantID: "e2e-tenant",
		Role:     "developer",
	})
	handler.Whoami(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	if active, _ := resp["active_team"].(string); active != "e2e-tenant" {
		t.Errorf("active_team = %q, want e2e-tenant", active)
	}
	teams, ok := resp["teams"].([]any)
	if !ok {
		t.Fatalf("expected teams to be []any, got %T (%v)", resp["teams"], resp["teams"])
	}
	if len(teams) != 2 {
		t.Errorf("expected 2 teams, got %d", len(teams))
	}
}

// TestWhoami_BackwardsCompatTeamField asserts the legacy `team` field is
// preserved (= active_team value) for one release of deprecation.
// Plan T2.5 RED #2.
func TestWhoami_BackwardsCompatTeamField(t *testing.T) {
	handler := NewAuthHandler(nil).WithMembershipLookup(&stubMembershipLookup{
		memberships: []middleware.TenantMembership{
			{TenantID: "tn-e2etest", Role: "owner"},
		},
	})

	rec := httptest.NewRecorder()
	req := makeWhoamiRequest(&middleware.UserContext{
		UserID:   "user-1",
		TenantID: "tn-e2etest",
		Role:     "owner",
	})
	handler.Whoami(rec, req)

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	if team, _ := resp["team"].(string); team != "tn-e2etest" {
		t.Errorf("legacy team field = %q, want tn-e2etest", team)
	}
	if active, _ := resp["active_team"].(string); active != "tn-e2etest" {
		t.Errorf("active_team = %q, want tn-e2etest", active)
	}
}

// TestWhoami_NoMembershipLookupDegrades asserts that without a MembershipLookup
// the handler still returns the active team (legacy single-team output).
// Plan T2.5 RED #3.
func TestWhoami_NoMembershipLookupDegrades(t *testing.T) {
	handler := NewAuthHandler(nil) // No membershipLookup

	rec := httptest.NewRecorder()
	req := makeWhoamiRequest(&middleware.UserContext{
		UserID:   "user-1",
		TenantID: "tn-solo",
		Role:     "owner",
	})
	handler.Whoami(rec, req)

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	if _, has := resp["teams"]; has {
		t.Errorf("expected no teams field without lookup, got %v", resp["teams"])
	}
	if active, _ := resp["active_team"].(string); active != "tn-solo" {
		t.Errorf("active_team = %q, want tn-solo", active)
	}
}

// TestWhoami_ActiveFlagOnCorrectTeam asserts the `active: true` flag is set
// exactly on the team matching the JWT's TenantID — addresses the EC-4
// concept of explicit active-team marker (no silent inference required).
// Plan T2.5 RED #4.
func TestWhoami_ActiveFlagOnCorrectTeam(t *testing.T) {
	handler := NewAuthHandler(nil).WithMembershipLookup(&stubMembershipLookup{
		memberships: []middleware.TenantMembership{
			{TenantID: "tn-a", Role: "developer"},
			{TenantID: "tn-b", Role: "owner"},
			{TenantID: "tn-c", Role: "viewer"},
		},
	})

	rec := httptest.NewRecorder()
	req := makeWhoamiRequest(&middleware.UserContext{
		UserID:   "user-1",
		TenantID: "tn-b",
		Role:     "owner",
	})
	handler.Whoami(rec, req)

	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)

	teams := resp["teams"].([]any)
	activeCount := 0
	for _, t := range teams {
		m := t.(map[string]any)
		if m["active"] == true {
			activeCount++
			if m["name"] != "tn-b" {
				return // fail: marked wrong team
			}
		}
	}
	if activeCount != 1 {
		t.Errorf("expected exactly 1 active team, got %d", activeCount)
	}
}
