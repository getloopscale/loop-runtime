// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package secretshttp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/routestest"
)

// The platform sets PORT on the app's container itself, and that value wins
// over one that comes from a secret. A secret of that name would be saved and
// never reach the app, so the write is refused with the reason.
func TestSecretBulk_RefusesANameThePlatformSets(t *testing.T) {
	store := &fakeSecretStoreT01{}
	h := NewSecretHandler(store, &lockCountingFake{})

	body, _ := json.Marshal(map[string]any{
		"project": "proj-x",
		"secrets": []map[string]any{
			{"name": "PORT", "value": "9999", "envs": []string{"production"}},
			{"name": "API_KEY", "value": "secret123", "envs": []string{"production"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/bulk", bytes.NewReader(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Bulk(w, req)

	var resp struct {
		Results []struct {
			Name      string `json:"name"`
			Status    string `json:"status"`
			Error     string `json:"error"`
			ErrorCode string `json:"error_code"`
		} `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if len(resp.Results) != 2 {
		t.Fatalf("results = %d; want 2 (body=%s)", len(resp.Results), w.Body.String())
	}
	port, other := resp.Results[0], resp.Results[1]
	if port.Status != "failed" || port.ErrorCode != "SECRET_NAME_RESERVED" {
		t.Errorf("PORT: status=%q code=%q; want failed / SECRET_NAME_RESERVED", port.Status, port.ErrorCode)
	}
	if !strings.Contains(port.Error, "PORT") || !strings.Contains(port.Error, "platform") {
		t.Errorf("PORT: the refusal does not say why: %q", port.Error)
	}
	if other.Status == "failed" {
		t.Errorf("API_KEY failed next to a reserved name: %q", other.Error)
	}
}

func TestSecretSet_RefusesANameThePlatformSets(t *testing.T) {
	store := &fakeSecretStoreT01{}
	h := NewSecretHandler(store, &lockCountingFake{})

	body := `{"project":"proj-x","environment":"production","name":"PORT","value":"9999"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Set(w, req)

	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "SECRET_NAME_RESERVED") {
		t.Fatalf("want 400 SECRET_NAME_RESERVED, got %d body=%s", w.Code, w.Body.String())
	}
}
