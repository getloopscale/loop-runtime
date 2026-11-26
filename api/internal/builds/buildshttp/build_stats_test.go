// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package buildshttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
)

// fakeStatsStore records what the handler passed and returns canned rows.
type fakeStatsStore struct {
	gotTenant string
	gotSince  time.Time
	rows      []BuildDayCount
	err       error
}

func (f *fakeStatsStore) BuildStatsByDay(_ context.Context, tenantID string, since time.Time) ([]BuildDayCount, error) {
	f.gotTenant = tenantID
	f.gotSince = since
	return f.rows, f.err
}

func statsRequest(tenant, window string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/builds/stats?window="+window, nil)
	if tenant != "" {
		ctx := context.WithValue(r.Context(), auth.UserContextKeyPublic(), &auth.UserContext{TenantID: tenant})
		r = r.WithContext(ctx)
	}
	return r
}

type statsBody struct {
	WindowDays int    `json:"window_days"`
	Timezone   string `json:"timezone"`
	ByDay      []struct {
		Date  string `json:"date"`
		Count int    `json:"count"`
	} `json:"by_day"`
	Last7dCount int `json:"last_7d_count"`
	TotalCount  int `json:"total_count"`
}

func doStats(t *testing.T, store BuildStatsStore, tenant, window string) (*httptest.ResponseRecorder, statsBody) {
	t.Helper()
	h := NewBuildStatsHandler(store)
	w := httptest.NewRecorder()
	h.Stats(w, statsRequest(tenant, window))
	var body statsBody
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
		}
	}
	return w, body
}

// THE #106/#111 PROTECTOR: the handler MUST scope the aggregate by the
// JWT-derived tenant and pass it to the store — never a client-supplied param.
func TestBuildStats_TenantIsolation_PassesJWTTenant(t *testing.T) {
	store := &fakeStatsStore{}
	// A ?tenant=/?project= param must be IGNORED — the handler only trusts the ctx.
	r := httptest.NewRequest(http.MethodGet, "/api/v1/builds/stats?window=7d&tenant=tn-attacker&project=other", nil)
	r = r.WithContext(context.WithValue(r.Context(), auth.UserContextKeyPublic(), &auth.UserContext{TenantID: "tn-victim"}))
	NewBuildStatsHandler(store).Stats(httptest.NewRecorder(), r)
	if store.gotTenant != "tn-victim" {
		t.Fatalf("isolation: store MUST be called with the JWT tenant tn-victim, got %q", store.gotTenant)
	}
}

func TestBuildStats_Unauthenticated_401(t *testing.T) {
	store := &fakeStatsStore{}
	w, _ := doStats(t, store, "", "7d") // no user context
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("fail-closed: want 401 on missing tenant, got %d", w.Code)
	}
	if store.gotTenant != "" {
		t.Errorf("fail-closed: store MUST NOT be queried without a tenant, got %q", store.gotTenant)
	}
}

func TestBuildStats_WindowEnum_Valid(t *testing.T) {
	for window, wantDays := range map[string]int{"7d": 7, "30d": 30, "90d": 90, "": 30} {
		store := &fakeStatsStore{}
		w, body := doStats(t, store, "tn-1", window)
		if w.Code != http.StatusOK {
			t.Fatalf("window=%q: want 200, got %d", window, w.Code)
		}
		if body.WindowDays != wantDays {
			t.Errorf("window=%q: want window_days=%d, got %d", window, wantDays, body.WindowDays)
		}
		if len(body.ByDay) != wantDays {
			t.Errorf("window=%q: by_day MUST be dense (%d entries), got %d", window, wantDays, len(body.ByDay))
		}
		if body.Timezone != "UTC" {
			t.Errorf("window=%q: timezone MUST be UTC, got %q", window, body.Timezone)
		}
	}
}

func TestBuildStats_WindowInvalid_Typed400(t *testing.T) {
	for _, window := range []string{"45d", "abc", "1", "7"} {
		store := &fakeStatsStore{}
		w, _ := doStats(t, store, "tn-1", window)
		if w.Code != http.StatusBadRequest {
			t.Errorf("window=%q: want typed 400, got %d", window, w.Code)
		}
	}
}

func TestBuildStats_EmptyState_DenseAllZero(t *testing.T) {
	store := &fakeStatsStore{rows: nil} // 0 builds
	w, body := doStats(t, store, "tn-1", "7d")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	if len(body.ByDay) != 7 {
		t.Fatalf("empty-state: by_day MUST be dense 7 entries, got %d", len(body.ByDay))
	}
	for i, b := range body.ByDay {
		if b.Count != 0 {
			t.Errorf("empty-state: by_day[%d] MUST be 0, got %d", i, b.Count)
		}
	}
	if body.TotalCount != 0 || body.Last7dCount != 0 {
		t.Errorf("empty-state: totals MUST be 0, got total=%d last7=%d", body.TotalCount, body.Last7dCount)
	}
	// ascending order
	for i := 1; i < len(body.ByDay); i++ {
		if body.ByDay[i-1].Date >= body.ByDay[i].Date {
			t.Errorf("by_day MUST be ascending: %s >= %s", body.ByDay[i-1].Date, body.ByDay[i].Date)
		}
	}
}

func TestBuildStats_UTCBucketing_TodayBucket(t *testing.T) {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	store := &fakeStatsStore{rows: []BuildDayCount{{Day: today, Count: 3}}}
	_, body := doStats(t, store, "tn-1", "7d")
	last := body.ByDay[len(body.ByDay)-1]
	if last.Date != today.Format("2006-01-02") || last.Count != 3 {
		t.Errorf("UTC bucketing: today's 3 builds MUST land in the last bucket, got %+v", last)
	}
	if body.TotalCount != 3 {
		t.Errorf("total: want 3, got %d", body.TotalCount)
	}
}

func TestBuildStats_Last7dConsistentWithByDay(t *testing.T) {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	// window=30d, counts on today (5) + 8 days ago (2, OUTSIDE the last-7d window).
	store := &fakeStatsStore{rows: []BuildDayCount{
		{Day: today, Count: 5},
		{Day: today.AddDate(0, 0, -8), Count: 2},
	}}
	_, body := doStats(t, store, "tn-1", "30d")
	// last_7d_count MUST equal the sum of the final 7 by_day buckets (today's 5 only).
	sumLast7 := 0
	for _, b := range body.ByDay[len(body.ByDay)-7:] {
		sumLast7 += b.Count
	}
	if body.Last7dCount != sumLast7 {
		t.Errorf("last_7d_count (%d) MUST equal sum of final 7 by_day buckets (%d)", body.Last7dCount, sumLast7)
	}
	if body.Last7dCount != 5 {
		t.Errorf("last_7d: only today's 5 are within 7d, got %d", body.Last7dCount)
	}
	if body.TotalCount != 7 {
		t.Errorf("total over 30d: want 7 (5+2), got %d", body.TotalCount)
	}
}
