// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package secrets

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/routestest"
)

// TestSecretsSetDeclaresProjectAsResourceID — B-059 FR-002.
//
// POST /api/v1/secrets hides its project in the query string, which the
// middleware also read. Reading it there meant every reader of the audit
// log had to know which routes hid the id where; the handler knows.
func TestSecretsSetDeclaresProjectAsResourceID(t *testing.T) {
	// Arrange.
	h := NewSecretHandler(&fakeSecretStore2{}, &lockCountingFake{})
	body := `{"name":"DB_URL","value":"redacted","environment":"production","app":"api"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets?project=my-app", bytes.NewBufferString(body))
	req = routestest.WithAuditSlot(routestest.WithAuth(req, routestest.DefaultUserContext()))

	// Act.
	h.Set(httptest.NewRecorder(), req)

	// Assert.
	if got := routestest.DeclaredResourceID(req); got != "my-app" {
		t.Fatalf("handler declared resource_id %q; want %q", got, "my-app")
	}
}
