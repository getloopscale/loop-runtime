// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package buildshttp

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/routestest"
)

// TestBuildsCreateDeclaresProjectAsResourceID — B-059 FR-002.
//
// POST /api/v1/builds carries its project in the body, so the audit
// middleware used to buffer up to 64 KiB of that body and scan it for
// `project_id`. The handler decoded the same field; now it declares it.
//
// The request below is rejected for a later reason (no apps) on purpose:
// the declaration must happen as soon as the id is known, not only on the
// happy path, or a rejected mutation would leave an unattributable row.
func TestBuildsCreateDeclaresProjectAsResourceID(t *testing.T) {
	// Arrange.
	h := NewBuildHandler(&fakeBuildOrch{}, slog.Default())
	body := `{"project_id":"my-monorepo","environment":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body))
	req = routestest.WithAuditSlot(routestest.WithAuth(req, routestest.DefaultUserContext()))

	// Act.
	h.Create(httptest.NewRecorder(), req)

	// Assert.
	if got := routestest.DeclaredResourceID(req); got != "my-monorepo" {
		t.Fatalf("handler declared resource_id %q; want %q", got, "my-monorepo")
	}
}
