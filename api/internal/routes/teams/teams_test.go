// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package teams

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/routestest"
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
	getUserFn   func(username string) (*TeamUserInfo, error)
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

func (f *fakeTeamStore) GetUserByUsername(_ context.Context, username string) (*TeamUserInfo, error) {
	if f.getUserFn != nil {
		return f.getUserFn(username)
	}
	return f.user, nil
}

// AUDIT-10: no test exercises this on the teams handler; provide a trivial
// impl so *fakeTeamStore continues to satisfy domain.TeamStore.
func (f *fakeTeamStore) GetUsernameByID(_ context.Context, _ string) (string, error) {
	return "", nil
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
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
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
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Add(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%s", w.Code, w.Body.String())
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
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
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
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Add(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTeams_Remove_HappyPath(t *testing.T) {
	// AUDIT-7: identity no longer constructed as "github-" + username;
	// test supplies getUserFn returning the opaque id directly.
	store := &fakeTeamStore{
		getUserFn: func(username string) (*TeamUserInfo, error) {
			if username == "bob" {
				return &TeamUserInfo{ID: "bob-id", Email: "b@t", DisplayName: "bob"}, nil
			}
			return nil, nil
		},
		getMemberFn: func(_, userID string) (*TeamMemberInfo, error) {
			if userID == "github-testuser" {
				return &TeamMemberInfo{Username: "testuser", Role: "owner"}, nil
			}
			return &TeamMemberInfo{Username: "bob", Role: "member"}, nil
		},
	}
	h := NewTeamHandler(store)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/teams/members/bob", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "username", "bob")
	w := httptest.NewRecorder()

	h.Remove(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

// CHG-2026-04-23-AUDIT-6: Remove must return 404 when the target user is not
// a team member. Previously returned 200 {"removed":"<username>"} even for
// fabricated usernames — CLI then showed "Removed <user> from team" (false).
func TestTeams_Remove_NotMember_Returns404(t *testing.T) {
	store := &fakeTeamStore{
		// AUDIT-7: user lookup returns nil for unknown usernames,
		// triggering the 404 before member lookup.
		getUserFn: func(_ string) (*TeamUserInfo, error) { return nil, nil },
		getMemberFn: func(_, userID string) (*TeamMemberInfo, error) {
			if userID == "github-testuser" {
				return &TeamMemberInfo{Username: "testuser", Role: "owner"}, nil
			}
			return nil, nil
		},
	}
	h := NewTeamHandler(store)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/teams/members/fake-user", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "username", "fake-user")
	w := httptest.NewRecorder()

	h.Remove(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 for non-member, got %d body=%s", w.Code, w.Body.String())
	}
	// Response must NOT contain {"removed":"..."} — that would be silent success.
	if bytes.Contains(w.Body.Bytes(), []byte(`"removed"`)) {
		t.Errorf("response must not claim removal for non-member: %s", w.Body.String())
	}
}

func TestTeams_Remove_CannotRemoveOwner(t *testing.T) {
	store := &fakeTeamStore{
		getUserFn: func(username string) (*TeamUserInfo, error) {
			if username == "targetowner" {
				return &TeamUserInfo{ID: "targetowner-id", Email: "o@t", DisplayName: "targetowner"}, nil
			}
			return nil, nil
		},
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
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "username", "targetowner")
	w := httptest.NewRecorder()

	h.Remove(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTeams_UpdateRole_HappyPath(t *testing.T) {
	store := &fakeTeamStore{
		getUserFn: func(username string) (*TeamUserInfo, error) {
			if username == "bob" {
				return &TeamUserInfo{ID: "bob-id", Email: "b@t", DisplayName: "bob"}, nil
			}
			return nil, nil
		},
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
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "username", "bob")
	w := httptest.NewRecorder()

	h.UpdateRole(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
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
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "username", "bob")
	w := httptest.NewRecorder()

	h.UpdateRole(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}
