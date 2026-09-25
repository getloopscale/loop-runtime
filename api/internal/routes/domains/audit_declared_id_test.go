// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package domains

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/routestest"
)

// TestDomainsCreateDeclaresProjectAsResourceID — B-059 FR-002.
//
// POST /api/v1/domains spelt its project `project`, not `project_id` —
// the second of the three spellings the transport layer maintained by
// hand. The handler declares it now.
func TestDomainsCreateDeclaresProjectAsResourceID(t *testing.T) {
	// Arrange.
	h := NewDomainHandler(&fakeDomainStore{}, &fakeDomainVerifier{}, &fakeDomainValidator{}, "apps.dev.loopzero.example")
	body := `{"domain":"example.com","project":"my-api","app":"web","environment":"production"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains", bytes.NewBufferString(body))
	req = routestest.WithAuditSlot(routestest.WithAuth(req, routestest.DefaultUserContext()))

	// Act.
	h.Create(httptest.NewRecorder(), req)

	// Assert.
	if got := routestest.DeclaredResourceID(req); got != "my-api" {
		t.Fatalf("handler declared resource_id %q; want %q", got, "my-api")
	}
}
