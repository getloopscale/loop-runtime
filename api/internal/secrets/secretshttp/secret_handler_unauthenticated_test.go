// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// A secret request with no authenticated caller is refused before any store
// call, on every route of the handler.
package secretshttp

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// =============================================================================
// SecretHandler — construction and setters
// =============================================================================

type fakeSecretStore2 struct{}

func (f *fakeSecretStore2) ListNames(_ context.Context, _, _, _ string) ([]SecretMetadata, error) {
	return nil, nil
}
func (f *fakeSecretStore2) Set(_ context.Context, _, _, _, _, _ string, _ []byte) (bool, error) {
	return true, nil
}
func (f *fakeSecretStore2) Delete(_ context.Context, _, _, _, _, _ string) (bool, error) {
	return true, nil
}

func TestSecretHandler_List_Unauthenticated(t *testing.T) {
	h := NewSecretHandler(&fakeSecretStore2{}, &lockCountingFake{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets", nil)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestSecretHandler_Set_Unauthenticated(t *testing.T) {
	h := NewSecretHandler(&fakeSecretStore2{}, &lockCountingFake{})
	body := `{"name":"DB_URL","value":"postgres://..."}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	h.Set(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestSecretHandler_Delete_Unauthenticated(t *testing.T) {
	h := NewSecretHandler(&fakeSecretStore2{}, &lockCountingFake{})
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/secrets/DB_URL", nil)
	w := httptest.NewRecorder()

	h.DeleteSecret(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}
