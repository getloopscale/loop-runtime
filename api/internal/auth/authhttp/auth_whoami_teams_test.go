// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package authhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/routestest"

	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
)

type stubMembershipLookup struct {
	memberships []auth.TenantMembership
	err         error
}

func (s *stubMembershipLookup) GetMemberships(_ context.Context, _ string) ([]auth.TenantMembership, error) {
	return s.memberships, s.err
}

type stubActiveTeamStore struct {
	active     string
	getErr     error
	setErr     error
	cleared    bool
	setCalls   int
	clearCalls int
}

func (s *stubActiveTeamStore) GetActiveTenant(_ context.Context, _ string) (string, error) {
	return s.active, s.getErr
}
func (s *stubActiveTeamStore) SetActiveTenant(_ context.Context, _, tenantID string) error {
	s.setCalls++
	if s.setErr != nil {
		return s.setErr
	}
	s.active = tenantID
	return nil
}
func (s *stubActiveTeamStore) ClearActiveTenant(_ context.Context, _ string) error {
	s.clearCalls++
	s.cleared = true
	s.active = ""
	return nil
}

func makeWhoamiRequest(uc *auth.UserContext) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/whoami", nil)
	if uc != nil {
		return routestest.WithAuth(req, uc)
	}
	return req
}

// TestWhoami_ReturnsTeamsArrayAndActiveTeam asserts plan T2.5 D8 — the
// response includes a `teams` array AND an `active_team` field when a
// MembershipLookup is wired.
// Plan T2.5 RED #1.
func TestWhoami_ReturnsTeamsArrayAndActiveTeam(t *testing.T) {
	handler := NewAuthHandler(nil).WithMembershipLookup(&stubMembershipLookup{
		memberships: []auth.TenantMembership{
			{TenantID: "tn-e2etest", Role: "owner"},
			{TenantID: "e2e-tenant", Role: "developer"},
		},
	})

	rec := httptest.NewRecorder()
	req := makeWhoamiRequest(&auth.UserContext{
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
		memberships: []auth.TenantMembership{
			{TenantID: "tn-e2etest", Role: "owner"},
		},
	})

	rec := httptest.NewRecorder()
	req := makeWhoamiRequest(&auth.UserContext{
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
	req := makeWhoamiRequest(&auth.UserContext{
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
		memberships: []auth.TenantMembership{
			{TenantID: "tn-a", Role: "developer"},
			{TenantID: "tn-b", Role: "owner"},
			{TenantID: "tn-c", Role: "viewer"},
		},
	})

	rec := httptest.NewRecorder()
	req := makeWhoamiRequest(&auth.UserContext{
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

// TestWhoami_ActiveStorePersistedWins asserts that when ActiveTeamStore has a
// persisted active_team, it overrides the JWT context selection (plan T2.5 D8
// full mode — closes F-S2-02 root cause).
// Plan T2.5 full RED #1.
func TestWhoami_ActiveStorePersistedWins(t *testing.T) {
	handler := NewAuthHandler(nil).
		WithMembershipLookup(&stubMembershipLookup{
			memberships: []auth.TenantMembership{
				{TenantID: "tn-a", Role: "developer"},
				{TenantID: "tn-b", Role: "owner"},
			},
		}).
		WithActiveTeamStore(&stubActiveTeamStore{active: "tn-b"})

	rec := httptest.NewRecorder()
	// JWT says active = tn-a, but persisted store says tn-b. Persisted wins.
	req := makeWhoamiRequest(&auth.UserContext{
		UserID:   "user-1",
		TenantID: "tn-a",
		Role:     "developer",
	})
	handler.Whoami(rec, req)

	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)

	if active, _ := resp["active_team"].(string); active != "tn-b" {
		t.Errorf("expected persisted active_team=tn-b to win, got %q", active)
	}
}

// TestWhoami_SelfHealsDanglingActiveTenant asserts EC-4 self-heal: when
// active_tenant_id references a tenant the user is not (or no longer) a
// member of, the handler clears the dangling reference + falls back to JWT
// context.
// Plan T2.5 full RED #2 — EC-4 closes self-heal symptom.
func TestWhoami_SelfHealsDanglingActiveTenant(t *testing.T) {
	store := &stubActiveTeamStore{active: "tn-deleted"}
	handler := NewAuthHandler(nil).
		WithMembershipLookup(&stubMembershipLookup{
			memberships: []auth.TenantMembership{
				{TenantID: "tn-a", Role: "developer"},
			},
		}).
		WithActiveTeamStore(store)

	rec := httptest.NewRecorder()
	req := makeWhoamiRequest(&auth.UserContext{
		UserID:   "user-1",
		TenantID: "tn-a",
		Role:     "developer",
	})
	handler.Whoami(rec, req)

	if !store.cleared {
		t.Errorf("expected ClearActiveTenant called for dangling reference, got cleared=false")
	}
	if store.clearCalls != 1 {
		t.Errorf("expected exactly 1 clear call, got %d", store.clearCalls)
	}

	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if active, _ := resp["active_team"].(string); active != "tn-a" {
		t.Errorf("expected fallback active_team=tn-a, got %q", active)
	}
}

// TestSwitchTeam_HappyPath asserts POST /team/switch updates active.
// Plan T2.5 full RED #3.
func TestSwitchTeam_HappyPath(t *testing.T) {
	store := &stubActiveTeamStore{}
	handler := NewAuthHandler(nil).
		WithMembershipLookup(&stubMembershipLookup{
			memberships: []auth.TenantMembership{
				{TenantID: "tn-a", Role: "developer"},
				{TenantID: "tn-b", Role: "owner"},
			},
		}).
		WithActiveTeamStore(store)

	body := bytes.NewBufferString(`{"name": "tn-b"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/team/switch", body)
	req = routestest.WithAuth(req, &auth.UserContext{
		UserID:   "user-1",
		TenantID: "tn-a",
		Role:     "developer",
	})
	rec := httptest.NewRecorder()
	handler.SwitchTeam(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if store.active != "tn-b" {
		t.Errorf("expected active=tn-b after switch, got %q", store.active)
	}
	if store.setCalls != 1 {
		t.Errorf("expected 1 SetActiveTenant call, got %d", store.setCalls)
	}
}

// TestSwitchTeam_NotMemberReturns403 asserts membership check enforced.
// Plan T2.5 full RED #4.
func TestSwitchTeam_NotMemberReturns403(t *testing.T) {
	store := &stubActiveTeamStore{}
	handler := NewAuthHandler(nil).
		WithMembershipLookup(&stubMembershipLookup{
			memberships: []auth.TenantMembership{
				{TenantID: "tn-a", Role: "developer"},
			},
		}).
		WithActiveTeamStore(store)

	body := bytes.NewBufferString(`{"name": "tn-not-mine"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/team/switch", body)
	req = routestest.WithAuth(req, &auth.UserContext{
		UserID:   "user-1",
		TenantID: "tn-a",
		Role:     "developer",
	})
	rec := httptest.NewRecorder()
	handler.SwitchTeam(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-member, got %d", rec.Code)
	}
	if store.setCalls != 0 {
		t.Errorf("expected no SetActiveTenant call on 403, got %d", store.setCalls)
	}
}

// TestSwitchTeam_InvalidBody asserts 400 on bad input.
func TestSwitchTeam_InvalidBody(t *testing.T) {
	handler := NewAuthHandler(nil).
		WithMembershipLookup(&stubMembershipLookup{}).
		WithActiveTeamStore(&stubActiveTeamStore{})

	body := bytes.NewBufferString(`not json`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/team/switch", body)
	req = routestest.WithAuth(req, &auth.UserContext{UserID: "u", TenantID: "t", Role: "r"})
	rec := httptest.NewRecorder()
	handler.SwitchTeam(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// TestSwitchTeam_MissingActiveStore asserts 501 when not configured.
func TestSwitchTeam_MissingActiveStore(t *testing.T) {
	handler := NewAuthHandler(nil) // no membership, no active store

	body := bytes.NewBufferString(`{"name": "tn-x"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/team/switch", body)
	req = routestest.WithAuth(req, &auth.UserContext{UserID: "u", TenantID: "t", Role: "r"})
	rec := httptest.NewRecorder()
	handler.SwitchTeam(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Errorf("expected 501 NotImplemented, got %d", rec.Code)
	}
}

// silence unused import warning for errors
var _ = errors.New
