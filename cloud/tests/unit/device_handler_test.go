// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/loopzero/loopzero/cloud/internal/auth"
	"github.com/loopzero/loopzero/cloud/internal/routes"
)

// mockHydraServer returns a test server that simulates Hydra's endpoints.
func mockHydraServer(t *testing.T, tokenReady bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth2/device/auth" && r.Method == "POST":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"device_code":              "test-device-code",
				"user_code":               "ABCD-1234",
				"verification_uri":         "http://localhost:4455/device",
				"verification_uri_complete": "http://localhost:4455/device?user_code=ABCD-1234",
				"expires_in":              300,
				"interval":                5,
			})

		case r.URL.Path == "/oauth2/token" && r.Method == "POST":
			_ = r.ParseForm()
			grantType := r.FormValue("grant_type")

			if grantType == "urn:ietf:params:oauth:grant-type:device_code" {
				if tokenReady {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"access_token":  "eyJ.test.jwt",
						"refresh_token": "refresh-token-123",
						"token_type":    "bearer",
						"expires_in":    3600,
					})
				} else {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"error":             "authorization_pending",
						"error_description": "The authorization request is still pending.",
					})
				}
				return
			}

			if grantType == "refresh_token" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"access_token":  "eyJ.refreshed.jwt",
					"refresh_token": "new-refresh-token",
					"token_type":    "bearer",
					"expires_in":    3600,
				})
				return
			}

			http.Error(w, "unsupported grant type", http.StatusBadRequest)

		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
}

func TestDeviceAuthorize(t *testing.T) {
	srv := mockHydraServer(t, false)
	defer srv.Close()

	hydra := auth.NewHydraClient(srv.URL, srv.URL, "loopzero-cli")
	handler := routes.NewDeviceHandler(hydra, "http://localhost:3000")

	req := httptest.NewRequest("POST", "/v1/auth/device/authorize", nil)
	w := httptest.NewRecorder()

	handler.Authorize(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["user_code"] != "ABCD-1234" {
		t.Errorf("user_code = %v, want ABCD-1234", resp["user_code"])
	}
	if resp["verification_uri"] == nil {
		t.Error("verification_uri missing")
	}
	if resp["device_code"] == nil {
		t.Error("device_code missing")
	}

	// Rule 3: no internal terms
	body := w.Body.String()
	for _, term := range []string{"Hydra", "hydra", "Kratos", "kratos", "OIDC"} {
		if strings.Contains(body, term) {
			t.Errorf("response contains internal term %q", term)
		}
	}
}

func TestDeviceTokenPending(t *testing.T) {
	srv := mockHydraServer(t, false)
	defer srv.Close()

	hydra := auth.NewHydraClient(srv.URL, srv.URL, "loopzero-cli")
	handler := routes.NewDeviceHandler(hydra, "http://localhost:3000")

	body := `{"device_code":"test-device-code"}`
	req := httptest.NewRequest("POST", "/v1/auth/device/token", strings.NewReader(body))
	w := httptest.NewRecorder()

	handler.Token(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["status"] != "pending" {
		t.Errorf("status = %v, want pending", resp["status"])
	}
}

func TestDeviceTokenSuccess(t *testing.T) {
	srv := mockHydraServer(t, true)
	defer srv.Close()

	hydra := auth.NewHydraClient(srv.URL, srv.URL, "loopzero-cli")
	handler := routes.NewDeviceHandler(hydra, "http://localhost:3000")

	body := `{"device_code":"test-device-code"}`
	req := httptest.NewRequest("POST", "/v1/auth/device/token", strings.NewReader(body))
	w := httptest.NewRecorder()

	handler.Token(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["access_token"] == nil {
		t.Error("access_token missing")
	}
	if resp["refresh_token"] == nil {
		t.Error("refresh_token missing")
	}

	// Rule 3: no internal terms in field names or error messages.
	// Token values themselves may contain "jwt" substrings — that's expected.
	for key := range resp {
		for _, term := range []string{"hydra", "kratos", "oidc"} {
			if strings.Contains(strings.ToLower(key), term) {
				t.Errorf("response key %q contains internal term %q", key, term)
			}
		}
	}
}

func TestDeviceTokenMissingBody(t *testing.T) {
	srv := mockHydraServer(t, false)
	defer srv.Close()

	hydra := auth.NewHydraClient(srv.URL, srv.URL, "loopzero-cli")
	handler := routes.NewDeviceHandler(hydra, "http://localhost:3000")

	req := httptest.NewRequest("POST", "/v1/auth/device/token", strings.NewReader("{}"))
	w := httptest.NewRecorder()

	handler.Token(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestRefreshToken(t *testing.T) {
	srv := mockHydraServer(t, false)
	defer srv.Close()

	hydra := auth.NewHydraClient(srv.URL, srv.URL, "loopzero-cli")
	handler := routes.NewDeviceHandler(hydra, "http://localhost:3000")

	body := `{"refresh_token":"refresh-token-123"}`
	req := httptest.NewRequest("POST", "/v1/auth/token/refresh", strings.NewReader(body))
	w := httptest.NewRecorder()

	handler.Refresh(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["access_token"] == nil {
		t.Error("access_token missing")
	}
}

func TestRefreshTokenMissingBody(t *testing.T) {
	srv := mockHydraServer(t, false)
	defer srv.Close()

	hydra := auth.NewHydraClient(srv.URL, srv.URL, "loopzero-cli")
	handler := routes.NewDeviceHandler(hydra, "http://localhost:3000")

	req := httptest.NewRequest("POST", "/v1/auth/token/refresh", strings.NewReader("{}"))
	w := httptest.NewRecorder()

	handler.Refresh(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}
