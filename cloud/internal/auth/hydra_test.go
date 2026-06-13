// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Plan J-05 iter 5 (2026-06-05) — pin the Hydra v2 nested-client_id
// contract for both login and consent challenge responses. Catches the
// LIVE regression where Hydra emitted {"client":{"client_id":"loopzero-cli"}}
// and our struct decoded the flat `client_id` to an empty string,
// causing consent handler to reject "Unauthorized client".

func TestGetConsentRequest_ParsesNestedClientID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"challenge": "abc",
			"client": map[string]interface{}{
				"client_id":   "loopzero-cli",
				"client_name": "LoopZero CLI",
			},
			"subject":                          "user-1",
			"requested_scope":                  []string{"openid"},
			"requested_access_token_audience":  []string{"loopzero-cloud"},
		})
	}))
	defer srv.Close()

	c := NewHydraClient(srv.URL, srv.URL, "loopzero-cli")
	got, err := c.GetConsentRequest("abc")
	if err != nil {
		t.Fatalf("GetConsentRequest: %v", err)
	}
	if got.ClientID != "loopzero-cli" {
		t.Fatalf("ClientID = %q, want %q", got.ClientID, "loopzero-cli")
	}
}

func TestGetConsentRequest_FlatClientIDStillWorks(t *testing.T) {
	// Backwards-compat — if any Hydra build still emits flat client_id,
	// we must NOT regress and return empty.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"challenge": "abc",
			"client_id": "loopzero-cli",
			"subject":   "user-1",
		})
	}))
	defer srv.Close()

	c := NewHydraClient(srv.URL, srv.URL, "loopzero-cli")
	got, err := c.GetConsentRequest("abc")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.ClientID != "loopzero-cli" {
		t.Fatalf("flat client_id lost: got %q", got.ClientID)
	}
}

func TestGetLoginRequest_ParsesNestedClientID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"challenge": "abc",
			"client": map[string]interface{}{
				"client_id": "loopzero-cli",
			},
			"subject": "user-1",
			"skip":    false,
		})
	}))
	defer srv.Close()

	c := NewHydraClient(srv.URL, srv.URL, "loopzero-cli")
	got, err := c.GetLoginRequest("abc")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.ClientID != "loopzero-cli" {
		t.Fatalf("ClientID = %q, want loopzero-cli", got.ClientID)
	}
}

func TestGetLoginRequest_FlatPreferredWhenBothPresent(t *testing.T) {
	// If both flat and nested are present, the flat value wins (existing
	// behavior — only promote nested when flat is empty).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"challenge": "abc",
			"client_id": "from-flat",
			"client": map[string]interface{}{
				"client_id": "from-nested",
			},
			"subject": "u",
		})
	}))
	defer srv.Close()

	c := NewHydraClient(srv.URL, srv.URL, "loopzero-cli")
	got, _ := c.GetLoginRequest("abc")
	if got.ClientID != "from-flat" {
		t.Fatalf("flat should win when both present: got %q", got.ClientID)
	}
}

func TestRegisterClient_IncludesAudienceWhitelist(t *testing.T) {
	// Plan J-05 iter 6 — F-03 LIVE bug: without audience on the registration
	// payload, refresh_token grants fail with "Requested audience 'loopzero-cloud'
	// has not been whitelisted by the OAuth 2.0 Client" because the original
	// device-flow consent set aud=[loopzero-cloud,loopzero-engine] and Hydra
	// re-validates against the client registration on every refresh.
	var captured map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" && strings.Contains(r.URL.Path, "/admin/clients/loopzero-cli") {
			_ = json.NewDecoder(r.Body).Decode(&captured)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"client_id":"loopzero-cli"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := NewHydraClient(srv.URL, srv.URL, "loopzero-cli")
	if err := c.RegisterClient([]string{"http://localhost:9876/auth"}); err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}

	aud, ok := captured["audience"].([]interface{})
	if !ok {
		t.Fatalf("audience field missing or wrong type: %T %v", captured["audience"], captured["audience"])
	}
	got := make([]string, len(aud))
	for i, v := range aud {
		got[i], _ = v.(string)
	}

	for _, want := range []string{"loopzero-cloud", "loopzero-engine"} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("audience missing %q (got %v)", want, got)
		}
	}
}

func TestGetConsentRequest_NeitherFlatNorNested_StaysEmpty(t *testing.T) {
	// When Hydra emits neither key, ClientID stays empty — consent
	// handler will (correctly) reject. This pins the failure mode.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"challenge":"abc","subject":"u"}`))
	}))
	defer srv.Close()

	c := NewHydraClient(srv.URL, srv.URL, "loopzero-cli")
	got, _ := c.GetConsentRequest("abc")
	if got.ClientID != "" {
		t.Fatalf("expected empty ClientID, got %q", got.ClientID)
	}
	if !strings.HasPrefix(got.Challenge, "abc") {
		t.Fatalf("challenge stamp lost: %q", got.Challenge)
	}
}
