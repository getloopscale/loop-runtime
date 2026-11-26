// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package teamshttp

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/routestest"
)

// TestTeamsAddDeclaresUsernameAsResourceID — B-059 FR-002.
//
// member.removed and member.role_changed already carried the username —
// it sits in their URL. member.added carried nothing, so the three actions
// on one member could not be joined. The body-snooper never helped here:
// the add body has no project key at all.
func TestTeamsAddDeclaresUsernameAsResourceID(t *testing.T) {
	// Arrange.
	store := &fakeTeamStore{member: &TeamMemberInfo{Username: "caller", Role: "owner"}}
	h := NewTeamHandler(store)
	body := `{"username":"newuser","role":"member"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/teams/members", bytes.NewBufferString(body))
	req = routestest.WithAuditSlot(routestest.WithAuth(req, routestest.DefaultUserContext()))

	// Act.
	h.Add(httptest.NewRecorder(), req)

	// Assert.
	if got := routestest.DeclaredResourceID(req); got != "newuser" {
		t.Fatalf("handler declared resource_id %q; want %q", got, "newuser")
	}
}
