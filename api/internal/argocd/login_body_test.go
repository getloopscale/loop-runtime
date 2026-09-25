// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argocd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestArgocdLoginSendsCredentialsAsValidJSON(t *testing.T) {
	// Arrange: a password with a control byte, which Go quoting (%q) renders
	// as \a — an escape JSON does not have.
	const password = "pa\ass\x01word"
	var got struct{ Username, Password string }
	var decodeErr error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		decodeErr = json.Unmarshal(raw, &got)
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "session"})
	}))
	defer srv.Close()
	p := newProvider(t, srv.URL)

	// Act
	if _, err := p.argocdLogin(context.Background(), "admin", password); err != nil {
		t.Fatalf("argocdLogin: %v", err)
	}

	// Assert
	if decodeErr != nil {
		t.Fatalf("login body is not valid JSON: %v", decodeErr)
	}
	if got.Username != "admin" || got.Password != password {
		t.Errorf("credentials did not round-trip: %+v", got)
	}
}
