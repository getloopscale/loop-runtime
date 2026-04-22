// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- Fake TeamStore ---

type fakeTeamStore struct {
	members     []TeamMemberInfo
	member      *TeamMemberInfo
	user        *TeamUserInfo
	addErr      error
	removeErr   error
	updateErr   error
	listErr     error
	getMemberFn func(tenantID, userID string) (*TeamMemberInfo, error)
}

func (f *fakeTeamStore) ListMembers(_ context.Context, _ string) ([]TeamMemberInfo, error) {
	return f.members, f.listErr
}

func (f *fakeTeamStore) AddMember(_ context.Context, _, _, _ string) error {
	return f.addErr
}

func (f *fakeTeamStore) RemoveMember(_ context.Context, _, _ string) error {
	return f.removeErr
}

func (f *fakeTeamStore) UpdateMemberRole(_ context.Context, _, _, _ string) error {
	return f.updateErr
}

func (f *fakeTeamStore) GetMember(_ context.Context, tenantID, userID string) (*TeamMemberInfo, error) {
	if f.getMemberFn != nil {
		return f.getMemberFn(tenantID, userID)
	}
	return f.member, nil
}

func (f *fakeTeamStore) GetUserByUsername(_ context.Context, _ string) (*TeamUserInfo, error) {
	return f.user, nil
}

// --- Tests ---

func TestTeams_List_HappyPath(t *testing.T) {
	store := &fakeTeamStore{
		members: []TeamMemberInfo{
			{Username: "alice", Email: "alice@test.com", Role: "owner", JoinedAt: "2026-01-01"},
			{Username: "bob", Email: "bob@test.com", Role: "member", JoinedAt: "2026-02-01"},
		},
	}
	h := NewTeamHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/teams/members", nil)
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string][]TeamMemberInfo
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp["members"]) != 2 {
		t.Fatalf("want 2 members, got %d", len(resp["members"]))
	}
}

func TestTeams_List_Unauthenticated(t *testing.T) {
	h := NewTeamHandler(&fakeTeamStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/teams/members", nil)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestTeams_Add_HappyPath(t *testing.T) {
	store := &fakeTeamStore{
		member: &TeamMemberInfo{Username: "caller", Role: "owner"},
		user:   &TeamUserInfo{ID: "github-newuser", Email: "new@test.com"},
	}
	h := NewTeamHandler(store)

	body := `{"username":"newuser","role":"member"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/teams/members", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Add(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTeams_Add_MemberCantAdd(t *testing.T) {
	store := &fakeTeamStore{
		member: &TeamMemberInfo{Username: "caller", Role: "member"}, // not owner/admin
		user:   &TeamUserInfo{ID: "github-newuser"},
	}
	h := NewTeamHandler(store)

	body := `{"username":"newuser","role":"member"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/teams/members", bytes.NewBufferString(body))
	req = withAuth(req, memberUserContext())
	w := httptest.NewRecorder()

	h.Add(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTeams_Add_CannotAssignOwnerRole(t *testing.T) {
	store := &fakeTeamStore{
		member: &TeamMemberInfo{Username: "caller", Role: "owner"},
		user:   &TeamUserInfo{ID: "github-newuser"},
	}
	h := NewTeamHandler(store)

	body := `{"username":"newuser","role":"owner"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/teams/members", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Add(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTeams_Add_UserNotFound(t *testing.T) {
	store := &fakeTeamStore{
		member: &TeamMemberInfo{Username: "caller", Role: "owner"},
		user:   nil, // user not found
	}
	h := NewTeamHandler(store)

	body := `{"username":"nonexistent","role":"member"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/teams/members", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Add(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTeams_Remove_HappyPath(t *testing.T) {
	store := &fakeTeamStore{
		getMemberFn: func(_, userID string) (*TeamMemberInfo, error) {
			if userID == "github-testuser" {
				return &TeamMemberInfo{Username: "testuser", Role: "owner"}, nil
			}
			return &TeamMemberInfo{Username: "bob", Role: "member"}, nil
		},
	}
	h := NewTeamHandler(store)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/teams/members/bob", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "username", "bob")
	w := httptest.NewRecorder()

	h.Remove(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTeams_Remove_CannotRemoveOwner(t *testing.T) {
	store := &fakeTeamStore{
		getMemberFn: func(_, userID string) (*TeamMemberInfo, error) {
			if userID == "github-testuser" {
				return &TeamMemberInfo{Username: "testuser", Role: "owner"}, nil
			}
			// target is also owner
			return &TeamMemberInfo{Username: "targetowner", Role: "owner"}, nil
		},
	}
	h := NewTeamHandler(store)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/teams/members/targetowner", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "username", "targetowner")
	w := httptest.NewRecorder()

	h.Remove(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTeams_UpdateRole_HappyPath(t *testing.T) {
	store := &fakeTeamStore{
		getMemberFn: func(_, userID string) (*TeamMemberInfo, error) {
			if userID == "github-testuser" {
				return &TeamMemberInfo{Username: "testuser", Role: "owner"}, nil
			}
			return &TeamMemberInfo{Username: "bob", Role: "member"}, nil
		},
	}
	h := NewTeamHandler(store)

	body := `{"role":"admin"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/teams/members/bob", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "username", "bob")
	w := httptest.NewRecorder()

	h.UpdateRole(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTeams_UpdateRole_OnlyOwnerCanChange(t *testing.T) {
	store := &fakeTeamStore{
		member: &TeamMemberInfo{Username: "admin-user", Role: "admin"}, // not owner
	}
	h := NewTeamHandler(store)

	body := `{"role":"admin"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/teams/members/bob", bytes.NewBufferString(body))
	req = withAuth(req, memberUserContext())
	req = withChiParam(req, "username", "bob")
	w := httptest.NewRecorder()

	h.UpdateRole(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTeams_UpdateRole_CannotAssignOwner(t *testing.T) {
	store := &fakeTeamStore{
		getMemberFn: func(_, userID string) (*TeamMemberInfo, error) {
			if userID == "github-testuser" {
				return &TeamMemberInfo{Username: "testuser", Role: "owner"}, nil
			}
			return &TeamMemberInfo{Username: "bob", Role: "member"}, nil
		},
	}
	h := NewTeamHandler(store)

	body := `{"role":"owner"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/teams/members/bob", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "username", "bob")
	w := httptest.NewRecorder()

	h.UpdateRole(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}
