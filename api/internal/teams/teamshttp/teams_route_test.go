// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package teamshttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/middleware/auth/authtest"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
	"github.com/getloopscale/loop-runtime/api/internal/teams/teamshttp"

	"github.com/go-chi/chi/v5"
)

// --- Fake TeamStore ---

type fakeTeamStore struct {
	members      map[string][]ports.TeamMemberInfo // tenantID -> members
	users        map[string]*ports.TeamUserInfo    // username -> user
	addErr       error
	removeErr    error
	updateErr    error
	listErr      error
	getMemberErr error
}

func newFakeTeamStore() *fakeTeamStore {
	return &fakeTeamStore{
		members: make(map[string][]ports.TeamMemberInfo),
		users:   make(map[string]*ports.TeamUserInfo),
	}
}

func (f *fakeTeamStore) ListMembers(_ context.Context, tenantID string) ([]ports.TeamMemberInfo, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	members := f.members[tenantID]
	if members == nil {
		return []ports.TeamMemberInfo{}, nil
	}
	return members, nil
}

func (f *fakeTeamStore) AddMember(_ context.Context, tenantID, userID, role string) error {
	if f.addErr != nil {
		return f.addErr
	}
	// Check if already a member (idempotent)
	for _, m := range f.members[tenantID] {
		if m.Username == stripGitHubPrefix(userID) {
			return nil
		}
	}
	f.members[tenantID] = append(f.members[tenantID], ports.TeamMemberInfo{
		Username: stripGitHubPrefix(userID),
		Email:    userID + "@test.com",
		Role:     role,
		JoinedAt: time.Now().UTC().Format(time.RFC3339),
	})
	return nil
}

func (f *fakeTeamStore) RemoveMember(_ context.Context, tenantID, userID string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	members := f.members[tenantID]
	for i, m := range members {
		if m.Username == stripGitHubPrefix(userID) {
			f.members[tenantID] = append(members[:i], members[i+1:]...)
			return nil
		}
	}
	return nil
}

func (f *fakeTeamStore) UpdateMemberRole(_ context.Context, tenantID, userID, newRole string) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	members := f.members[tenantID]
	for i, m := range members {
		if m.Username == stripGitHubPrefix(userID) {
			f.members[tenantID][i].Role = newRole
			return nil
		}
	}
	return ports.ErrMemberNotFound
}

func (f *fakeTeamStore) GetMember(_ context.Context, tenantID, userID string) (*ports.TeamMemberInfo, error) {
	if f.getMemberErr != nil {
		return nil, f.getMemberErr
	}
	for _, m := range f.members[tenantID] {
		if m.Username == stripGitHubPrefix(userID) {
			return &m, nil
		}
	}
	return nil, nil
}

func (f *fakeTeamStore) GetUserByUsername(_ context.Context, username string) (*ports.TeamUserInfo, error) {
	u, ok := f.users[username]
	if !ok {
		return nil, nil
	}
	return u, nil
}

// CHG-2026-04-23-AUDIT-20: trivial stub — GetUsernameByID was added to
// the TeamStore domain interface in AUDIT-10 (migration 000037 backs it
// with users.username column) but this fake was never updated, silently
// breaking the whole test build. Return the id as-is; tests that
// need specific behavior can override via a future mapped field.
func (f *fakeTeamStore) GetUsernameByID(_ context.Context, id string) (string, error) {
	return id, nil
}

func stripGitHubPrefix(id string) string {
	if len(id) > 7 && id[:7] == "github-" {
		return id[7:]
	}
	return id
}

// --- Helpers to set up a store with an owner ---
//
// CHG-2026-04-23-AUDIT-20: addMember mirrors each TeamMember entry into
// the users map so GetUserByUsername (introduced in AUDIT-7 to replace
// the github- prefix strip) can resolve the user id during Remove and
// UpdateRole. Previously only members was populated, which silently
// broke once the handlers started routing username→id through the
// users table.

func addMember(s *fakeTeamStore, tenantID, userID, role string) {
	username := stripGitHubPrefix(userID)
	s.members[tenantID] = append(s.members[tenantID], ports.TeamMemberInfo{
		Username: username,
		Email:    userID + "@test.com",
		Role:     role,
		JoinedAt: time.Now().UTC().Format(time.RFC3339),
	})
	s.users[username] = &ports.TeamUserInfo{
		ID:          userID,
		Email:       userID + "@test.com",
		DisplayName: username,
	}
}

func storeWithOwner(tenantID, ownerUserID string) *fakeTeamStore {
	s := newFakeTeamStore()
	addMember(s, tenantID, ownerUserID, "owner")
	s.users["newuser"] = &ports.TeamUserInfo{
		ID:          "github-newuser",
		Email:       "newuser@test.com",
		DisplayName: "New User",
	}
	return s
}

func storeWithOwnerAndAdmin(tenantID, ownerUserID, adminUserID string) *fakeTeamStore {
	s := storeWithOwner(tenantID, ownerUserID)
	addMember(s, tenantID, adminUserID, "admin")
	return s
}

func storeWithOwnerAndMember(tenantID, ownerUserID, memberUserID string) *fakeTeamStore {
	s := storeWithOwner(tenantID, ownerUserID)
	addMember(s, tenantID, memberUserID, "member")
	return s
}

// --- Tests ---

func TestTeamList(t *testing.T) {
	t.Run("GET /api/v1/teams/members → 200 with members list", func(t *testing.T) {
		store := storeWithOwnerAndMember("acme", "github-lucas", "github-bob")
		handler := teamshttp.NewTeamHandler(store)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/teams/members", nil).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		handler.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var result map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&result)
		members, ok := result["members"].([]interface{})
		if !ok {
			t.Fatal("expected 'members' array in response")
		}
		if len(members) != 2 {
			t.Fatalf("expected 2 members, got %d", len(members))
		}

		// Verify usernames have no "github-" prefix
		for _, m := range members {
			member := m.(map[string]interface{})
			username := member["username"].(string)
			if len(username) > 7 && username[:7] == "github-" {
				t.Errorf("username should not have github- prefix, got %s", username)
			}
		}
	})

	t.Run("GET /api/v1/teams/members → 401 without auth", func(t *testing.T) {
		store := newFakeTeamStore()
		handler := teamshttp.NewTeamHandler(store)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/teams/members", nil)
		rec := httptest.NewRecorder()

		handler.List(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("GET /api/v1/teams/members → 200 with empty list when no members", func(t *testing.T) {
		store := newFakeTeamStore()
		handler := teamshttp.NewTeamHandler(store)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/teams/members", nil).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		handler.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var result map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&result)
		members := result["members"].([]interface{})
		if len(members) != 0 {
			t.Fatalf("expected 0 members, got %d", len(members))
		}
	})
}

func TestTeamAdd(t *testing.T) {
	t.Run("POST /api/v1/teams/members → 201 when owner adds member", func(t *testing.T) {
		store := storeWithOwner("acme", "github-lucas")
		handler := teamshttp.NewTeamHandler(store)

		body := `{"username":"newuser","role":"member"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/teams/members", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Add(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /api/v1/teams/members → 201 when admin adds member", func(t *testing.T) {
		store := storeWithOwnerAndAdmin("acme", "github-lucas", "github-admin")
		handler := teamshttp.NewTeamHandler(store)

		body := `{"username":"newuser","role":"member"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/teams/members", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-admin", "acme", "admin"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Add(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /api/v1/teams/members → 401 without auth", func(t *testing.T) {
		store := newFakeTeamStore()
		handler := teamshttp.NewTeamHandler(store)

		body := `{"username":"newuser","role":"member"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/teams/members", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Add(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("POST /api/v1/teams/members → 400 with invalid role", func(t *testing.T) {
		store := storeWithOwner("acme", "github-lucas")
		handler := teamshttp.NewTeamHandler(store)

		body := `{"username":"newuser","role":"superadmin"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/teams/members", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Add(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// SECURITY R-01: privilege escalation — "owner" role must be rejected via API
	t.Run("POST /api/v1/teams/members → 400 rejects owner role (privilege escalation)", func(t *testing.T) {
		store := storeWithOwner("acme", "github-lucas")
		store.users["github-hacker"] = &ports.TeamUserInfo{ID: "github-hacker", Email: "h@x.com"}
		handler := teamshttp.NewTeamHandler(store)

		body := `{"username":"hacker","role":"owner"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/teams/members", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Add(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("SECURITY: owner role should be rejected, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /api/v1/teams/members → 404 when user not found", func(t *testing.T) {
		store := storeWithOwner("acme", "github-lucas")
		// Do NOT add "unknownuser" to store.users
		handler := teamshttp.NewTeamHandler(store)

		body := `{"username":"unknownuser","role":"member"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/teams/members", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Add(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /api/v1/teams/members → 400 with missing username", func(t *testing.T) {
		store := storeWithOwner("acme", "github-lucas")
		handler := teamshttp.NewTeamHandler(store)

		body := `{"role":"member"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/teams/members", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Add(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestTeamRemove(t *testing.T) {
	t.Run("DELETE /api/v1/teams/members/{username} → 200 when admin removes member", func(t *testing.T) {
		store := storeWithOwnerAndAdmin("acme", "github-lucas", "github-admin")
		addMember(store, "acme", "github-bob", "member")
		handler := teamshttp.NewTeamHandler(store)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/teams/members/bob", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("username", "bob")
		ctx := authtest.Context("github-admin", "acme", "admin")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		handler.Remove(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("DELETE /api/v1/teams/members/{username} → 403 when member tries to remove", func(t *testing.T) {
		store := storeWithOwnerAndMember("acme", "github-lucas", "github-bob")
		handler := teamshttp.NewTeamHandler(store)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/teams/members/lucas", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("username", "lucas")
		ctx := authtest.Context("github-bob", "acme", "member")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		handler.Remove(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("DELETE /api/v1/teams/members/{username} → 403 when trying to remove owner", func(t *testing.T) {
		store := storeWithOwnerAndAdmin("acme", "github-lucas", "github-admin")
		handler := teamshttp.NewTeamHandler(store)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/teams/members/lucas", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("username", "lucas")
		ctx := authtest.Context("github-admin", "acme", "admin")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		handler.Remove(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("DELETE /api/v1/teams/members/{username} → 403 when admin removes another admin", func(t *testing.T) {
		store := storeWithOwnerAndAdmin("acme", "github-lucas", "github-admin1")
		addMember(store, "acme", "github-admin2", "admin")
		handler := teamshttp.NewTeamHandler(store)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/teams/members/admin2", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("username", "admin2")
		ctx := authtest.Context("github-admin1", "acme", "admin")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		handler.Remove(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("DELETE /api/v1/teams/members/{username} → 401 without auth", func(t *testing.T) {
		store := newFakeTeamStore()
		handler := teamshttp.NewTeamHandler(store)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/teams/members/bob", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("username", "bob")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()

		handler.Remove(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})
}

func TestTeamUpdateRole(t *testing.T) {
	t.Run("PATCH /api/v1/teams/members/{username} → 200 when owner changes role", func(t *testing.T) {
		store := storeWithOwnerAndMember("acme", "github-lucas", "github-bob")
		handler := teamshttp.NewTeamHandler(store)

		body := `{"role":"admin"}`
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/teams/members/bob", bytes.NewBufferString(body))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("username", "bob")
		ctx := authtest.Context("github-lucas", "acme", "owner")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.UpdateRole(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("PATCH /api/v1/teams/members/{username} → 403 when trying to change owner role", func(t *testing.T) {
		store := storeWithOwner("acme", "github-lucas")
		handler := teamshttp.NewTeamHandler(store)

		body := `{"role":"admin"}`
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/teams/members/lucas", bytes.NewBufferString(body))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("username", "lucas")
		ctx := authtest.Context("github-lucas", "acme", "owner")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.UpdateRole(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("PATCH /api/v1/teams/members/{username} → 404 when member not found", func(t *testing.T) {
		store := storeWithOwner("acme", "github-lucas")
		handler := teamshttp.NewTeamHandler(store)

		body := `{"role":"admin"}`
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/teams/members/nonexistent", bytes.NewBufferString(body))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("username", "nonexistent")
		ctx := authtest.Context("github-lucas", "acme", "owner")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.UpdateRole(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("PATCH /api/v1/teams/members/{username} → 400 with invalid role", func(t *testing.T) {
		store := storeWithOwnerAndMember("acme", "github-lucas", "github-bob")
		handler := teamshttp.NewTeamHandler(store)

		body := `{"role":"superadmin"}`
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/teams/members/bob", bytes.NewBufferString(body))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("username", "bob")
		ctx := authtest.Context("github-lucas", "acme", "owner")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.UpdateRole(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// SECURITY R-01: privilege escalation — "owner" role must be rejected in UpdateRole
	t.Run("PATCH /api/v1/teams/members/{username} → 400 rejects owner role (privilege escalation)", func(t *testing.T) {
		store := storeWithOwnerAndMember("acme", "github-lucas", "github-bob")
		handler := teamshttp.NewTeamHandler(store)

		body := `{"role":"owner"}`
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/teams/members/bob", bytes.NewBufferString(body))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("username", "bob")
		ctx := authtest.Context("github-lucas", "acme", "owner")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.UpdateRole(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("SECURITY: owner role should be rejected in UpdateRole, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("PATCH /api/v1/teams/members/{username} → 401 without auth", func(t *testing.T) {
		store := newFakeTeamStore()
		handler := teamshttp.NewTeamHandler(store)

		body := `{"role":"admin"}`
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/teams/members/bob", bytes.NewBufferString(body))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("username", "bob")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.UpdateRole(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})
}
