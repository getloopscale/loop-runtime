// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package environmentshttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/getloopscale/loopzero/api/internal/k8s"
	"github.com/getloopscale/loopzero/api/internal/k8s/tenancy"
	"github.com/getloopscale/loopzero/api/internal/routestest"
)

// A tenant that has signed up but not created a project has no Tenant CR yet.
// GET /api/v1/environments answers it the way the other list routes answer an
// empty tenant: 200 with an empty list. The handler runs against the real
// Tenant CR manager so the missing CR is what it actually reads.
func TestEnvs_List_TenantWithoutTenantCRIsAnEmptyList(t *testing.T) {
	scheme, err := k8s.NewScheme()
	if err != nil {
		t.Fatalf("NewScheme: %v", err)
	}
	mgr := &tenancy.TenantCRManager{Client: fake.NewClientBuilder().WithScheme(scheme).Build()}
	h := NewEnvironmentHandler(mgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Environments []EnvironmentEntry `json:"environments"`
		Total        *int               `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if resp.Environments == nil || len(resp.Environments) != 0 {
		t.Fatalf("want \"environments\": [], got body=%s", w.Body.String())
	}
	if resp.Total == nil || *resp.Total != 0 {
		t.Fatalf("want \"total\": 0, got body=%s", w.Body.String())
	}
}
