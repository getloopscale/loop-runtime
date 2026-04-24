// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package enforcement

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// ---------------------------------------------------------------------------
// PlanLimiter: nil / dev-mode paths
// ---------------------------------------------------------------------------

func TestCheckLimit_NilLimiter(t *testing.T) {
	var limiter *PlanLimiter
	result := limiter.CheckLimit("tn-test", "projects", 5)
	if !result.Allowed {
		t.Error("nil limiter should allow everything")
	}
	if result.Plan != "dev" {
		t.Errorf("nil limiter plan should be 'dev', got %q", result.Plan)
	}
}

func TestCheckLimit_NilDB(t *testing.T) {
	limiter := &PlanLimiter{db: nil}
	result := limiter.CheckLimit("tn-test", "projects", 5)
	if !result.Allowed {
		t.Error("nil DB should allow everything")
	}
}

// ---------------------------------------------------------------------------
// PlanLimiter: enterprise plan is unlimited
// ---------------------------------------------------------------------------

func TestCheckLimit_EnterprisePlan_AlwaysAllowed(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	// getTenantPlan returns "enterprise"
	mock.ExpectQuery("SELECT plan FROM subscriptions").
		WithArgs("tn-ent").
		WillReturnRows(sqlmock.NewRows([]string{"plan"}).AddRow("enterprise"))

	limiter := NewPlanLimiter(db, testLogger())
	result := limiter.CheckLimit("tn-ent", "projects", 9999)

	if !result.Allowed {
		t.Error("enterprise plan should always be allowed")
	}
	if result.Plan != "enterprise" {
		t.Errorf("Plan = %q, want enterprise", result.Plan)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// PlanLimiter: starter plan limits
// ---------------------------------------------------------------------------

func TestCheckLimit_StarterPlan_UnderLimit_Allowed(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT plan FROM subscriptions").
		WithArgs("tn-starter").
		WillReturnRows(sqlmock.NewRows([]string{"plan"}).AddRow("starter"))

	mock.ExpectQuery("SELECT hard_limit, soft_limit FROM plan_limits").
		WithArgs("starter", "projects").
		WillReturnRows(sqlmock.NewRows([]string{"hard_limit", "soft_limit"}).AddRow(1, nil))

	limiter := NewPlanLimiter(db, testLogger())
	result := limiter.CheckLimit("tn-starter", "projects", 0)

	if !result.Allowed {
		t.Error("starter with 0 projects should be allowed (limit=1)")
	}
	if result.Plan != "starter" {
		t.Errorf("Plan = %q, want starter", result.Plan)
	}
	if result.Limit != 1 {
		t.Errorf("Limit = %d, want 1", result.Limit)
	}
}

func TestCheckLimit_StarterPlan_AtLimit_Blocked(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT plan FROM subscriptions").
		WithArgs("tn-starter").
		WillReturnRows(sqlmock.NewRows([]string{"plan"}).AddRow("starter"))

	mock.ExpectQuery("SELECT hard_limit, soft_limit FROM plan_limits").
		WithArgs("starter", "projects").
		WillReturnRows(sqlmock.NewRows([]string{"hard_limit", "soft_limit"}).AddRow(1, nil))

	limiter := NewPlanLimiter(db, testLogger())
	result := limiter.CheckLimit("tn-starter", "projects", 1)

	if result.Allowed {
		t.Error("starter at limit (1/1) should be blocked")
	}
	if result.Current != 1 {
		t.Errorf("Current = %d, want 1", result.Current)
	}
	if result.Limit != 1 {
		t.Errorf("Limit = %d, want 1", result.Limit)
	}
}

// ---------------------------------------------------------------------------
// PlanLimiter: pro/growth plan limits
// ---------------------------------------------------------------------------

func TestCheckLimit_ProPlan_UnderLimit_Allowed(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT plan FROM subscriptions").
		WithArgs("tn-pro").
		WillReturnRows(sqlmock.NewRows([]string{"plan"}).AddRow("pro"))

	mock.ExpectQuery("SELECT hard_limit, soft_limit FROM plan_limits").
		WithArgs("pro", "projects").
		WillReturnRows(sqlmock.NewRows([]string{"hard_limit", "soft_limit"}).AddRow(3, nil))

	limiter := NewPlanLimiter(db, testLogger())
	result := limiter.CheckLimit("tn-pro", "projects", 2)

	if !result.Allowed {
		t.Error("pro with 2/3 projects should be allowed")
	}
}

func TestCheckLimit_ProPlan_AtLimit_Blocked(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT plan FROM subscriptions").
		WithArgs("tn-pro").
		WillReturnRows(sqlmock.NewRows([]string{"plan"}).AddRow("pro"))

	mock.ExpectQuery("SELECT hard_limit, soft_limit FROM plan_limits").
		WithArgs("pro", "projects").
		WillReturnRows(sqlmock.NewRows([]string{"hard_limit", "soft_limit"}).AddRow(3, nil))

	limiter := NewPlanLimiter(db, testLogger())
	result := limiter.CheckLimit("tn-pro", "projects", 3)

	if result.Allowed {
		t.Error("pro at limit (3/3) should be blocked")
	}
}

// ---------------------------------------------------------------------------
// PlanLimiter: builds per month
// ---------------------------------------------------------------------------

func TestCheckLimit_BuildsPerMonth_Enforced(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT plan FROM subscriptions").
		WithArgs("tn-test").
		WillReturnRows(sqlmock.NewRows([]string{"plan"}).AddRow("starter"))

	mock.ExpectQuery("SELECT hard_limit, soft_limit FROM plan_limits").
		WithArgs("starter", "builds_per_month").
		WillReturnRows(sqlmock.NewRows([]string{"hard_limit", "soft_limit"}).AddRow(100, nil))

	limiter := NewPlanLimiter(db, testLogger())
	result := limiter.CheckLimit("tn-test", "builds_per_month", 100)

	if result.Allowed {
		t.Error("at builds_per_month limit should be blocked")
	}
	if result.Metric != "builds_per_month" {
		t.Errorf("Metric = %q, want builds_per_month", result.Metric)
	}
}

// ---------------------------------------------------------------------------
// PlanLimiter: seats limit
// ---------------------------------------------------------------------------

func TestCheckLimit_SeatsLimit_Enforced(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT plan FROM subscriptions").
		WithArgs("tn-team").
		WillReturnRows(sqlmock.NewRows([]string{"plan"}).AddRow("team"))

	mock.ExpectQuery("SELECT hard_limit, soft_limit FROM plan_limits").
		WithArgs("team", "seats").
		WillReturnRows(sqlmock.NewRows([]string{"hard_limit", "soft_limit"}).AddRow(10, nil))

	limiter := NewPlanLimiter(db, testLogger())
	result := limiter.CheckLimit("tn-team", "seats", 10)

	if result.Allowed {
		t.Error("at seats limit should be blocked")
	}
}

// ---------------------------------------------------------------------------
// PlanLimiter: environments limit
// ---------------------------------------------------------------------------

func TestCheckLimit_EnvironmentsLimit_Enforced(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT plan FROM subscriptions").
		WithArgs("tn-test").
		WillReturnRows(sqlmock.NewRows([]string{"plan"}).AddRow("starter"))

	mock.ExpectQuery("SELECT hard_limit, soft_limit FROM plan_limits").
		WithArgs("starter", "environments").
		WillReturnRows(sqlmock.NewRows([]string{"hard_limit", "soft_limit"}).AddRow(2, nil))

	limiter := NewPlanLimiter(db, testLogger())
	result := limiter.CheckLimit("tn-test", "environments", 2)

	if result.Allowed {
		t.Error("at environments limit should be blocked")
	}
}

func TestCheckLimit_EnvironmentsLimit_UnderLimit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT plan FROM subscriptions").
		WithArgs("tn-test").
		WillReturnRows(sqlmock.NewRows([]string{"plan"}).AddRow("pro"))

	mock.ExpectQuery("SELECT hard_limit, soft_limit FROM plan_limits").
		WithArgs("pro", "environments").
		WillReturnRows(sqlmock.NewRows([]string{"hard_limit", "soft_limit"}).AddRow(5, nil))

	limiter := NewPlanLimiter(db, testLogger())
	result := limiter.CheckLimit("tn-test", "environments", 3)

	if !result.Allowed {
		t.Error("under environments limit should be allowed")
	}
}

// ---------------------------------------------------------------------------
// PlanLimiter: soft limit (overage) behavior
// ---------------------------------------------------------------------------

func TestCheckLimit_SoftLimit_OverageAllowed(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT plan FROM subscriptions").
		WithArgs("tn-team").
		WillReturnRows(sqlmock.NewRows([]string{"plan"}).AddRow("team"))

	// Hard limit 10, soft limit 8 — at 8, allowed but SoftOver
	mock.ExpectQuery("SELECT hard_limit, soft_limit FROM plan_limits").
		WithArgs("team", "seats").
		WillReturnRows(sqlmock.NewRows([]string{"hard_limit", "soft_limit"}).AddRow(10, 8))

	limiter := NewPlanLimiter(db, testLogger())
	result := limiter.CheckLimit("tn-team", "seats", 9)

	if !result.Allowed {
		t.Error("soft over should still be allowed")
	}
	if !result.SoftOver {
		t.Error("SoftOver should be true when at/above soft limit but below hard")
	}
}

// ---------------------------------------------------------------------------
// PlanLimiter: negative hard limit (unlimited metric)
// ---------------------------------------------------------------------------

func TestCheckLimit_NegativeHardLimit_Unlimited(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT plan FROM subscriptions").
		WithArgs("tn-test").
		WillReturnRows(sqlmock.NewRows([]string{"plan"}).AddRow("pro"))

	// hard_limit NULL → parsed as -1 → unlimited
	mock.ExpectQuery("SELECT hard_limit, soft_limit FROM plan_limits").
		WithArgs("pro", "builds_per_month").
		WillReturnRows(sqlmock.NewRows([]string{"hard_limit", "soft_limit"}).AddRow(nil, nil))

	limiter := NewPlanLimiter(db, testLogger())
	result := limiter.CheckLimit("tn-test", "builds_per_month", 999)

	if !result.Allowed {
		t.Error("NULL hard limit should mean unlimited")
	}
}

// ---------------------------------------------------------------------------
// PlanLimiter: missing subscription row → fallback to starter
// ---------------------------------------------------------------------------

func TestCheckLimit_MissingSubscription_FallbackStarter(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	// No subscription row
	mock.ExpectQuery("SELECT plan FROM subscriptions").
		WithArgs("tn-unknown").
		WillReturnRows(sqlmock.NewRows([]string{"plan"})) // empty result

	// getLimits for starter plan
	mock.ExpectQuery("SELECT hard_limit, soft_limit FROM plan_limits").
		WithArgs("starter", "projects").
		WillReturnRows(sqlmock.NewRows([]string{"hard_limit", "soft_limit"}).AddRow(1, nil))

	fallbackCalled := false
	limiter := NewPlanLimiter(db, testLogger())
	limiter.OnFallbackStarter = func() { fallbackCalled = true }

	result := limiter.CheckLimit("tn-unknown", "projects", 0)

	if result.Plan != "starter" {
		t.Errorf("Plan = %q, want starter (fallback)", result.Plan)
	}
	if !fallbackCalled {
		t.Error("OnFallbackStarter callback should have been called")
	}
	if !result.Allowed {
		t.Error("0/1 should be allowed on starter")
	}
}

// ---------------------------------------------------------------------------
// PlanLimiter: missing plan_limits row → deny by default
// ---------------------------------------------------------------------------

func TestCheckLimit_MissingLimitsRow_DeniedByDefault(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT plan FROM subscriptions").
		WithArgs("tn-test").
		WillReturnRows(sqlmock.NewRows([]string{"plan"}).AddRow("starter"))

	// No plan_limits row → returns empty
	mock.ExpectQuery("SELECT hard_limit, soft_limit FROM plan_limits").
		WithArgs("starter", "unknown_metric").
		WillReturnRows(sqlmock.NewRows([]string{"hard_limit", "soft_limit"}))

	limiter := NewPlanLimiter(db, testLogger())
	result := limiter.CheckLimit("tn-test", "unknown_metric", 0)

	if result.Allowed {
		t.Error("missing plan_limits row should deny by default (fail closed)")
	}
}

// ---------------------------------------------------------------------------
// GetBuildTimeout
// ---------------------------------------------------------------------------

func TestGetBuildTimeout_NilLimiter(t *testing.T) {
	var limiter *PlanLimiter
	timeout := limiter.GetBuildTimeout("tn-test")
	if timeout != 1800 {
		t.Errorf("nil limiter timeout = %d, want 1800", timeout)
	}
}

func TestGetBuildTimeout_NilDB(t *testing.T) {
	limiter := &PlanLimiter{db: nil}
	timeout := limiter.GetBuildTimeout("tn-test")
	if timeout != 1800 {
		t.Errorf("nil DB timeout = %d, want 1800", timeout)
	}
}

func TestGetBuildTimeout_FromDB(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT plan FROM subscriptions").
		WithArgs("tn-pro").
		WillReturnRows(sqlmock.NewRows([]string{"plan"}).AddRow("pro"))

	mock.ExpectQuery("SELECT hard_limit, soft_limit FROM plan_limits").
		WithArgs("pro", "build_timeout_seconds").
		WillReturnRows(sqlmock.NewRows([]string{"hard_limit", "soft_limit"}).AddRow(3600, nil))

	limiter := NewPlanLimiter(db, testLogger())
	timeout := limiter.GetBuildTimeout("tn-pro")

	if timeout != 3600 {
		t.Errorf("timeout = %d, want 3600", timeout)
	}
}

func TestGetBuildTimeout_MissingRow_Default(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT plan FROM subscriptions").
		WithArgs("tn-test").
		WillReturnRows(sqlmock.NewRows([]string{"plan"}).AddRow("starter"))

	// No row for build_timeout_seconds
	mock.ExpectQuery("SELECT hard_limit, soft_limit FROM plan_limits").
		WithArgs("starter", "build_timeout_seconds").
		WillReturnRows(sqlmock.NewRows([]string{"hard_limit", "soft_limit"}))

	limiter := NewPlanLimiter(db, testLogger())
	timeout := limiter.GetBuildTimeout("tn-test")

	if timeout != 1800 {
		t.Errorf("timeout = %d, want 1800 (default)", timeout)
	}
}

// ---------------------------------------------------------------------------
// GetRateLimit
// ---------------------------------------------------------------------------

func TestGetRateLimit_NilLimiter(t *testing.T) {
	var limiter *PlanLimiter
	rate := limiter.GetRateLimit("tn-test")
	if rate != 300 {
		t.Errorf("nil limiter rate = %d, want 300", rate)
	}
}

func TestGetRateLimit_FromDB(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT plan FROM subscriptions").
		WithArgs("tn-team").
		WillReturnRows(sqlmock.NewRows([]string{"plan"}).AddRow("team"))

	mock.ExpectQuery("SELECT hard_limit, soft_limit FROM plan_limits").
		WithArgs("team", "api_requests_per_min").
		WillReturnRows(sqlmock.NewRows([]string{"hard_limit", "soft_limit"}).AddRow(600, nil))

	limiter := NewPlanLimiter(db, testLogger())
	rate := limiter.GetRateLimit("tn-team")

	if rate != 600 {
		t.Errorf("rate = %d, want 600", rate)
	}
}

func TestGetRateLimit_MissingRow_Default(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT plan FROM subscriptions").
		WithArgs("tn-test").
		WillReturnRows(sqlmock.NewRows([]string{"plan"}).AddRow("starter"))

	mock.ExpectQuery("SELECT hard_limit, soft_limit FROM plan_limits").
		WithArgs("starter", "api_requests_per_min").
		WillReturnRows(sqlmock.NewRows([]string{"hard_limit", "soft_limit"}))

	limiter := NewPlanLimiter(db, testLogger())
	rate := limiter.GetRateLimit("tn-test")

	if rate != 60 {
		t.Errorf("rate = %d, want 60 (default)", rate)
	}
}

// ---------------------------------------------------------------------------
// FormatLimitError
// ---------------------------------------------------------------------------

func TestFormatLimitError_Fields(t *testing.T) {
	result := LimitResult{
		Allowed: false,
		Plan:    "starter",
		Metric:  "projects",
		Current: 1,
		Limit:   1,
	}
	errMap := FormatLimitError(result, "https://example.com/billing")

	if errMap["code"] != "PLAN_LIMIT_EXCEEDED" {
		t.Errorf("code = %v, want PLAN_LIMIT_EXCEEDED", errMap["code"])
	}
	msg, ok := errMap["message"].(string)
	if !ok || msg == "" {
		t.Error("message should be non-empty string")
	}
	if errMap["plan"] != "starter" {
		t.Errorf("plan = %v, want starter", errMap["plan"])
	}
	if errMap["current"] != 1 {
		t.Errorf("current = %v, want 1", errMap["current"])
	}
	if errMap["max"] != 1 {
		t.Errorf("max = %v, want 1", errMap["max"])
	}
	if errMap["upgrade_url"] != "https://example.com/billing" {
		t.Errorf("upgrade_url wrong")
	}

	// No internal terms in message
	for _, term := range []string{"ArgoCD", "Gitea", "MinIO", "CRD", "tenant_id"} {
		if contains(msg, term) {
			t.Errorf("message contains internal term %q: %s", term, msg)
		}
	}
}

func TestFormatLimitError_DisplayNames(t *testing.T) {
	tests := []struct {
		plan   string
		metric string
		want   string
	}{
		{"starter", "projects", "Starter"},
		{"pro", "builds_per_month", "Pro"},
		{"team", "seats", "Team"},
	}
	for _, tt := range tests {
		result := LimitResult{Plan: tt.plan, Metric: tt.metric, Limit: 1, Current: 1}
		errMap := FormatLimitError(result, "")
		msg := errMap["message"].(string)
		if !contains(msg, tt.want) {
			t.Errorf("message for plan %q should contain %q: %s", tt.plan, tt.want, msg)
		}
	}
}

func TestFormatLimitError_AllMetricDisplayNames(t *testing.T) {
	metrics := map[string]string{
		"projects":             "projects",
		"services_per_project": "services per project",
		"environments":         "environments",
		"builds_per_month":     "builds per month",
		"seats":                "team members",
		"secrets_per_env":      "secrets per environment",
		"unknown_metric":       "unknown_metric", // fallback
	}
	for metric, wantDisplay := range metrics {
		result := LimitResult{Plan: "starter", Metric: metric, Limit: 5, Current: 5}
		errMap := FormatLimitError(result, "")
		msg := errMap["message"].(string)
		if !contains(msg, wantDisplay) {
			t.Errorf("metric %q: message should contain %q, got: %s", metric, wantDisplay, msg)
		}
	}
}

func TestFormatLimitError_UnknownPlan_UsesRawName(t *testing.T) {
	result := LimitResult{Plan: "custom", Metric: "projects", Limit: 50, Current: 50}
	errMap := FormatLimitError(result, "")
	msg := errMap["message"].(string)
	if !contains(msg, "custom") {
		t.Errorf("unknown plan should use raw name: %s", msg)
	}
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

func TestMiddleware_NilLimiter_Passes(t *testing.T) {
	innerCalled := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		innerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	mw := Middleware(nil, OperationCheck{
		Metric:  "projects",
		CountFn: func(tenantID string) int { return 999 },
	}, func(r *http.Request) string { return "tn-test" }, "")

	handler := mw(inner)
	req := httptest.NewRequest("POST", "/api/v1/projects", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !innerCalled {
		t.Error("nil limiter should pass through")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// TestMiddleware_EmptyTenantPasses lives in middleware_test.go — removed here
// to fix a pre-existing duplicate-symbol compile error that blocked B15 tests.

func TestMiddleware_BlocksOverLimit_402(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT plan FROM subscriptions").
		WithArgs("tn-blocked").
		WillReturnRows(sqlmock.NewRows([]string{"plan"}).AddRow("starter"))

	mock.ExpectQuery("SELECT hard_limit, soft_limit FROM plan_limits").
		WithArgs("starter", "projects").
		WillReturnRows(sqlmock.NewRows([]string{"hard_limit", "soft_limit"}).AddRow(1, nil))

	limiter := NewPlanLimiter(db, testLogger())

	innerCalled := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		innerCalled = true
	})

	mw := Middleware(
		limiter,
		OperationCheck{
			Metric:  "projects",
			CountFn: func(tenantID string) int { return 1 }, // at limit
		},
		func(r *http.Request) string { return "tn-blocked" },
		"https://billing.example.com",
	)

	handler := mw(inner)
	req := httptest.NewRequest("POST", "/api/v1/projects", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if innerCalled {
		t.Error("inner handler should NOT be called when limit exceeded")
	}
	if rec.Code != http.StatusPaymentRequired {
		t.Errorf("status = %d, want 402", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestMiddleware_PassesUnderLimit_200(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT plan FROM subscriptions").
		WithArgs("tn-ok").
		WillReturnRows(sqlmock.NewRows([]string{"plan"}).AddRow("pro"))

	mock.ExpectQuery("SELECT hard_limit, soft_limit FROM plan_limits").
		WithArgs("pro", "projects").
		WillReturnRows(sqlmock.NewRows([]string{"hard_limit", "soft_limit"}).AddRow(3, nil))

	limiter := NewPlanLimiter(db, testLogger())

	innerCalled := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		innerCalled = true
		w.WriteHeader(http.StatusCreated)
	})

	mw := Middleware(
		limiter,
		OperationCheck{
			Metric:  "projects",
			CountFn: func(tenantID string) int { return 1 }, // under limit
		},
		func(r *http.Request) string { return "tn-ok" },
		"",
	)

	handler := mw(inner)
	req := httptest.NewRequest("POST", "/api/v1/projects", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !innerCalled {
		t.Error("inner handler should be called when under limit")
	}
	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// CountRows
// ---------------------------------------------------------------------------

func TestCountRows_NilLimiter(t *testing.T) {
	fn := CountRows(nil, "SELECT COUNT(*) FROM projects WHERE tenant_id = $1")
	count := fn("tn-test")
	if count != 0 {
		t.Errorf("nil limiter count = %d, want 0", count)
	}
}

func TestCountRows_NilDB(t *testing.T) {
	limiter := &PlanLimiter{db: nil}
	fn := CountRows(limiter, "SELECT COUNT(*) FROM projects WHERE tenant_id = $1")
	count := fn("tn-test")
	if count != 0 {
		t.Errorf("nil DB count = %d, want 0", count)
	}
}

func TestCountRows_ReturnsCount(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT COUNT").
		WithArgs("tn-test").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(7))

	limiter := NewPlanLimiter(db, testLogger())
	fn := CountRows(limiter, "SELECT COUNT(*) FROM projects WHERE tenant_id = $1")
	count := fn("tn-test")

	if count != 7 {
		t.Errorf("count = %d, want 7", count)
	}
}

func TestCountBuildsThisMonth_NilLimiter(t *testing.T) {
	fn := CountBuildsThisMonth(nil)
	count := fn("tn-test")
	if count != 0 {
		t.Errorf("nil limiter count = %d, want 0", count)
	}
}

func TestCountBuildsThisMonth_NilDB(t *testing.T) {
	limiter := &PlanLimiter{db: nil}
	fn := CountBuildsThisMonth(limiter)
	count := fn("tn-test")
	if count != 0 {
		t.Errorf("nil DB count = %d, want 0", count)
	}
}

// ---------------------------------------------------------------------------
// CheckValue — B15 (CHG-2026-04-23-B15 / MEET-2026-04-23-059)
// ---------------------------------------------------------------------------
// Unlike CheckLimit (which answers "is this operation within limits given
// current usage?"), CheckValue answers "is this REQUESTED value within the
// plan cap?". Used for per-resource intent checks like replica counts.

func TestCheckValue_NilLimiter_Allowed(t *testing.T) {
	var limiter *PlanLimiter
	result := limiter.CheckValue("tn-test", "max_replicas_per_service", 999)
	if !result.Allowed {
		t.Error("nil limiter should allow everything")
	}
}

func TestCheckValue_EnterprisePlan_AlwaysAllowed(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT plan FROM subscriptions").
		WithArgs("tn-ent").
		WillReturnRows(sqlmock.NewRows([]string{"plan"}).AddRow("enterprise"))

	limiter := NewPlanLimiter(db, testLogger())
	result := limiter.CheckValue("tn-ent", "max_replicas_per_service", 1000)

	if !result.Allowed {
		t.Error("enterprise plan should always be allowed")
	}
}

func TestCheckValue_StarterCap2_Requested2_Allowed(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT plan FROM subscriptions").
		WithArgs("tn-starter").
		WillReturnRows(sqlmock.NewRows([]string{"plan"}).AddRow("starter"))

	mock.ExpectQuery("SELECT hard_limit, soft_limit FROM plan_limits").
		WithArgs("starter", "max_replicas_per_service").
		WillReturnRows(sqlmock.NewRows([]string{"hard_limit", "soft_limit"}).AddRow(2, nil))

	limiter := NewPlanLimiter(db, testLogger())
	result := limiter.CheckValue("tn-starter", "max_replicas_per_service", 2)

	if !result.Allowed {
		t.Errorf("requested==cap should be allowed, got %+v", result)
	}
	if result.Limit != 2 {
		t.Errorf("Limit = %d, want 2", result.Limit)
	}
	if result.Current != 2 {
		t.Errorf("Current (= requested value) = %d, want 2", result.Current)
	}
}

func TestCheckValue_StarterCap2_Requested3_Blocked(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT plan FROM subscriptions").
		WithArgs("tn-starter").
		WillReturnRows(sqlmock.NewRows([]string{"plan"}).AddRow("starter"))

	mock.ExpectQuery("SELECT hard_limit, soft_limit FROM plan_limits").
		WithArgs("starter", "max_replicas_per_service").
		WillReturnRows(sqlmock.NewRows([]string{"hard_limit", "soft_limit"}).AddRow(2, nil))

	limiter := NewPlanLimiter(db, testLogger())
	result := limiter.CheckValue("tn-starter", "max_replicas_per_service", 3)

	if result.Allowed {
		t.Error("requested > cap should be blocked")
	}
	if result.Limit != 2 || result.Current != 3 {
		t.Errorf("expected Limit=2 Current=3, got %+v", result)
	}
	if result.Plan != "starter" {
		t.Errorf("Plan = %q, want starter", result.Plan)
	}
}

func TestCheckValue_NullHardLimit_Unlimited(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT plan FROM subscriptions").
		WithArgs("tn-team").
		WillReturnRows(sqlmock.NewRows([]string{"plan"}).AddRow("team"))

	mock.ExpectQuery("SELECT hard_limit, soft_limit FROM plan_limits").
		WithArgs("team", "max_replicas_per_service").
		WillReturnRows(sqlmock.NewRows([]string{"hard_limit", "soft_limit"}).AddRow(nil, nil))

	limiter := NewPlanLimiter(db, testLogger())
	result := limiter.CheckValue("tn-team", "max_replicas_per_service", 100)

	if !result.Allowed {
		t.Error("NULL hard_limit should mean unlimited")
	}
}

func TestCheckValue_MissingPlanLimitsRow_DenyByDefault(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT plan FROM subscriptions").
		WithArgs("tn-starter").
		WillReturnRows(sqlmock.NewRows([]string{"plan"}).AddRow("starter"))

	// No row returned → hardLimit==0 per getLimits contract → fail closed.
	mock.ExpectQuery("SELECT hard_limit, soft_limit FROM plan_limits").
		WithArgs("starter", "max_replicas_per_service").
		WillReturnRows(sqlmock.NewRows([]string{"hard_limit", "soft_limit"}))

	limiter := NewPlanLimiter(db, testLogger())
	result := limiter.CheckValue("tn-starter", "max_replicas_per_service", 5)

	if result.Allowed {
		t.Error("missing plan_limits row should deny by default (fail closed)")
	}
}

func TestCountBuildsThisMonth_ReturnsCount(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT COUNT").
		WithArgs("tn-test", sqlmock.AnyArg()). // period start is dynamic
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(42))

	limiter := NewPlanLimiter(db, testLogger())
	fn := CountBuildsThisMonth(limiter)
	count := fn("tn-test")

	if count != 42 {
		t.Errorf("count = %d, want 42", count)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
