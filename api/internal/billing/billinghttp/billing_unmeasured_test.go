// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package billinghttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/routestest"

	planpkg "github.com/getloopscale/contracts/plan"
)

// getBillingBody performs the request and returns the RAW body, once.
//
// Decoding drains the recorder's buffer, and a second reader would see an empty
// body — a well-known way for an assertion about JSON to assert nothing at all.
func getBillingBody(t *testing.T, h *BillingHandler) []byte {
	t.Helper()
	req := routestest.WithAuth(
		httptest.NewRequest(http.MethodGet, "/api/v1/billing", nil),
		routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.Get(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	return w.Body.Bytes()
}

// An empty counter table must not be served as measured zero usage.
//
// B-033, re-measured live on 2026-09-05 against k3d-loopzero-core: `cost_counters`
// holds **0 rows**, the running `loopzero-cloud` Deployment carries 19 environment
// variables and none matching `PROM|COST`, and the three PromQL queries the
// aggregator would run return empty vectors because `loopzero_egress_bytes_total`
// and `loopzero_docr_pulls_total` are emitted by no repository. Nothing fills the
// table, and on the present code nothing will.
//
// The enforcement gate has told the two cases apart since `reason="unmeasured"`
// (B-033, third bullet). The READ side did not: `GET /api/v1/billing` collapsed
// `found=false` into `used: 0` and served "you used 0 of 100" for a cap that had
// never evaluated anything. That is the same defect as the third bullet, on the
// surface a customer reads — the one place the degradation stayed silent after
// the gate had learned to speak.
//
// `measured` is the field that separates them. Go's zero value is `false`, so a
// decoder that ignores the field reads the PESSIMISTIC claim ("this was not
// measured"), never the optimistic one. The polarity is deliberate: this item's
// failure mode is an inert protection that looks healthy.
func TestBilling_MissingCounterIsNotServedAsMeasuredZero(t *testing.T) {
	// Empty store = exactly the state measured on app-dev.
	cost := &fakeCostSource{counters: map[planpkg.CostMetric]planpkg.CostCounter{}}
	body := getBillingBody(t, NewBillingHandler(fakePlanStore{plan: "starter"}, fakeCustomerStore{}).WithCostSource(cost))

	var resp BillingResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Usage) != 3 {
		t.Fatalf("all three metrics stay listed with their caps; got %d: %+v",
			len(resp.Usage), resp.Usage)
	}
	for metric, entry := range resp.Usage {
		if entry.Measured {
			t.Errorf("%s: measured=true with no counter row — the empty table is being "+
				"served as a measurement, which is the defect B-033 measures", metric)
		}
		if entry.Used != 0 {
			t.Errorf("%s: used=%d — with no row the displayed value stays 0; what changes "+
				"is the claim made about it", metric, entry.Used)
		}
	}

	// The field has to be IN the JSON, not merely on the struct: the panel reads
	// the response, not the Go type.
	var raw struct {
		Usage map[string]map[string]any `json:"usage"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if _, present := raw.Usage["build_minutes"]["measured"]; !present {
		t.Errorf("`measured` is absent from the build_minutes JSON (%+v) — a struct that "+
			"knows the difference and a response that does not say it are the same silent "+
			"degradation, one layer up", raw.Usage["build_minutes"])
	}
}

// The measured path still declares itself measured. Without this, the new field
// could be a constant, and a constant field distinguishes nothing.
func TestBilling_PresentCounterDeclaresItselfMeasured(t *testing.T) {
	cost := &fakeCostSource{
		counters: map[planpkg.CostMetric]planpkg.CostCounter{
			planpkg.MetricBuildMinutes: {Value: 7},
		},
	}
	body := getBillingBody(t, NewBillingHandler(fakePlanStore{plan: "starter"}, fakeCustomerStore{}).WithCostSource(cost))

	var resp BillingResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if bm := resp.Usage["build_minutes"]; !bm.Measured || bm.Used != 7 {
		t.Errorf("build_minutes with a counter row: %+v, want used=7 measured=true", bm)
	}
	if dp := resp.Usage["docr_pulls"]; dp.Measured {
		t.Errorf("docr_pulls with no counter row: %+v, want measured=false", dp)
	}
}
