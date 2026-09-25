// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package environments

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/routestest"
)

// TestEnvironmentsCreateDeclaresProjectAsResourceID — B-059 FR-002.
//
// POST /api/v1/environments carried its project in the body under the
// `project` key. Declaring it keeps environment.created joinable to the
// project it was created under.
func TestEnvironmentsCreateDeclaresProjectAsResourceID(t *testing.T) {
	// Arrange.
	h := NewEnvironmentHandler(&fakeEnvMgr{})
	body := `{"name":"staging","project":"my-api"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(body))
	req = routestest.WithAuditSlot(routestest.WithAuth(req, routestest.DefaultUserContext()))

	// Act.
	h.Create(httptest.NewRecorder(), req)

	// Assert.
	if got := routestest.DeclaredResourceID(req); got != "my-api" {
		t.Fatalf("handler declared resource_id %q; want %q", got, "my-api")
	}
}
