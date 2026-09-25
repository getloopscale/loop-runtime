// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package projects

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/routestest"
)

// TestProjectsCreateDeclaresNameAsResourceID — B-059 FR-002.
//
// Same asymmetry as team members: project.deleted carries the name from
// its URL, project.created carried nothing, and no body key the snooper
// knew about appears in a create-project body.
func TestProjectsCreateDeclaresNameAsResourceID(t *testing.T) {
	// Arrange.
	h := NewProjectHandler(&fakeProjectStore{}, &fakeTenantCRMgr{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(`{"name":"api"}`))
	req = routestest.WithAuditSlot(routestest.WithAuth(req, routestest.DefaultUserContext()))

	// Act.
	h.Create(httptest.NewRecorder(), req)

	// Assert.
	if got := routestest.DeclaredResourceID(req); got != "api" {
		t.Fatalf("handler declared resource_id %q; want %q", got, "api")
	}
}
