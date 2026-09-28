// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package vault

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A response body that breaks mid-read is a transport failure, and its cause
// must survive. Returning the partial bytes as a successful response turned a
// connection reset into a confusing JSON decode error far from its origin.
func TestTransitClient_ResponseCutMidBodyReturnsTheReadError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth/approle/login", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"auth":{"client_token":"hvs.t"}}`)
	})
	mux.HandleFunc("/v1/transit/decrypt/", func(w http.ResponseWriter, _ *http.Request) {
		// Promise more bytes than are sent, then drop the connection.
		w.Header().Set("Content-Length", "200")
		fmt.Fprint(w, `{"data":{"plaintext":"cG9z`)
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("test server does not support hijacking")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatalf("hijack: %v", err)
		}
		if err := conn.Close(); err != nil {
			t.Errorf("close hijacked conn: %v", err)
		}
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	c, err := NewTransitClient(TransitConfig{Addr: srv.URL, RoleID: "r", SecretID: "s", HTTPClient: srv.Client()})
	if err != nil {
		t.Fatalf("NewTransitClient: %v", err)
	}

	_, err = c.Decrypt(context.Background(), "tn-A", "vault:v1:x")

	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err = %v, want it to carry io.ErrUnexpectedEOF from the body read", err)
	}
}

// A pin that is not a SHA-256 digest can never match any certificate, so it
// is refused at construction instead of failing every handshake later.
func TestNewTransitClient_RefusesAPinThatIsNotASHA256Digest(t *testing.T) {
	_, err := NewTransitClient(TransitConfig{
		Addr: "https://vault.example", RoleID: "r", SecretID: "s",
		SPKIPinSHA256: strings.Repeat("ab", 16), // 16 bytes, not 32
	})

	if err == nil || !strings.Contains(err.Error(), "32 bytes") {
		t.Fatalf("err = %v, want a refusal naming the 32-byte SHA-256 length", err)
	}
}
