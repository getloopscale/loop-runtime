// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package domain

import (
	"context"
	"errors"
	"testing"
)

// stubPlanTierStore allows tests to dictate (raw, err) without spinning
// up a real database connection.
type stubPlanTierStore struct {
	raw string
	err error
}

func (s stubPlanTierStore) GetTenantPlan(_ context.Context, _ string) (string, error) {
	return s.raw, s.err
}

func TestResolvePlanTier_NewTenantNoRowReturnsStarter(t *testing.T) {
	// ADR D4 documented fallback: brand-new tenant with NO row gets
	// Starter with nil error. This is the ONLY default-to-Starter path.
	got, err := ResolvePlanTier(context.Background(),
		stubPlanTierStore{raw: "", err: nil}, "tn-new")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != PlanTierStarter {
		t.Errorf("expected Starter for new tenant, got %q", got)
	}
}

func TestResolvePlanTier_RecognizedValues(t *testing.T) {
	cases := []struct {
		raw  string
		want PlanTier
	}{
		{"starter", PlanTierStarter},
		{"pro", PlanTierPro},
		{"team", PlanTierTeam},
		{"enterprise", PlanTierEnterprise},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			got, err := ResolvePlanTier(context.Background(),
				stubPlanTierStore{raw: tc.raw}, "tn-x")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

// TestResolvePlanTier_LegacyGrowthMapsToPro — the pre-MEET-2026-05-12
// "growth" CRD enum value is mapped to canonical Pro per ADR D4.
// Existing growth tenants continue working without manual DB migration.
func TestResolvePlanTier_LegacyGrowthMapsToPro(t *testing.T) {
	got, err := ResolvePlanTier(context.Background(),
		stubPlanTierStore{raw: "growth"}, "tn-legacy")
	if err != nil {
		t.Fatalf("legacy growth must NOT error: %v", err)
	}
	if got != PlanTierPro {
		t.Errorf("expected legacy growth → Pro, got %q", got)
	}
}

// TestResolvePlanTier_UnrecognizedValueErrors — H7 closure invariant:
// an unrecognized value MUST surface an error, NEVER silently default
// to Starter. This is the contract `feedback_swallowed_silent_errors_pattern`
// codifies.
func TestResolvePlanTier_UnrecognizedValueErrors(t *testing.T) {
	got, err := ResolvePlanTier(context.Background(),
		stubPlanTierStore{raw: "platinum-vip"}, "tn-x")
	if !errors.Is(err, ErrUnknownPlanTier) {
		t.Errorf("expected ErrUnknownPlanTier, got %v", err)
	}
	if got != PlanTierUnknown {
		t.Errorf("expected PlanTierUnknown sentinel, got %q", got)
	}
}

// TestResolvePlanTier_StoreErrorPropagates — transport errors propagate
// unchanged. Caller decides retry vs 503; ResolvePlanTier never swallows.
func TestResolvePlanTier_StoreErrorPropagates(t *testing.T) {
	want := errors.New("connection refused")
	got, err := ResolvePlanTier(context.Background(),
		stubPlanTierStore{err: want}, "tn-x")
	if !errors.Is(err, want) {
		t.Errorf("expected store error to propagate, got %v", err)
	}
	if got != PlanTierUnknown {
		t.Errorf("expected PlanTierUnknown on store error, got %q", got)
	}
}

func TestResolvePlanTier_RejectsNilStore(t *testing.T) {
	got, err := ResolvePlanTier(context.Background(), nil, "tn-x")
	if err == nil {
		t.Error("nil store must surface an error")
	}
	if got != PlanTierUnknown {
		t.Errorf("expected PlanTierUnknown on nil store, got %q", got)
	}
}

func TestResolvePlanTier_RejectsEmptyTenantID(t *testing.T) {
	got, err := ResolvePlanTier(context.Background(),
		stubPlanTierStore{raw: "pro"}, "")
	if err == nil {
		t.Error("empty tenantID must surface an error")
	}
	if got != PlanTierUnknown {
		t.Errorf("expected PlanTierUnknown on empty tenantID, got %q", got)
	}
}

func TestPlanTier_IsValid(t *testing.T) {
	cases := []struct {
		tier PlanTier
		want bool
	}{
		{PlanTierStarter, true},
		{PlanTierPro, true},
		{PlanTierTeam, true},
		{PlanTierEnterprise, true},
		{PlanTierUnknown, false},
		{PlanTier("growth"), false}, // legacy value is NOT valid post-resolution
		{PlanTier("vip"), false},
	}
	for _, tc := range cases {
		t.Run(string(tc.tier), func(t *testing.T) {
			if got := tc.tier.IsValid(); got != tc.want {
				t.Errorf("%q.IsValid() = %v; want %v", tc.tier, got, tc.want)
			}
		})
	}
}

func TestPlanTier_StringMatchesUnderlying(t *testing.T) {
	if PlanTierPro.String() != "pro" {
		t.Errorf("String() != underlying value: %q", PlanTierPro.String())
	}
	if PlanTierUnknown.String() != "" {
		t.Errorf("Unknown must serialize as empty string, got %q",
			PlanTierUnknown.String())
	}
}

func TestPlanTierDisplayName_NamesEveryTierAndKeepsUnknownValuesVisible(t *testing.T) {
	cases := map[PlanTier]string{
		PlanTierStarter:    "Starter",
		PlanTierPro:        "Pro",
		PlanTierTeam:       "Team",
		PlanTierEnterprise: "Enterprise",
		PlanTier("growth"): "growth",
	}
	for tier, want := range cases {
		if got := tier.DisplayName(); got != want {
			t.Errorf("PlanTier(%q).DisplayName() = %q, want %q", tier, got, want)
		}
	}
}

func TestParsePlanTier_AcceptsCanonicalValues(t *testing.T) {
	for _, raw := range []string{"starter", "pro", "team", "enterprise"} {
		got, err := ParsePlanTier(raw)
		if err != nil || got.String() != raw {
			t.Errorf("ParsePlanTier(%q) = (%q, %v), want (%q, nil)", raw, got, err, raw)
		}
	}
}

func TestParsePlanTier_MapsLegacyGrowthToPro(t *testing.T) {
	got, err := ParsePlanTier("growth")
	if err != nil || got != PlanTierPro {
		t.Errorf("ParsePlanTier(growth) = (%q, %v), want (pro, nil)", got, err)
	}
}

func TestParsePlanTier_RejectsUnknownWithoutDefaultingToStarter(t *testing.T) {
	for _, raw := range []string{"", "gold", "TEAM"} {
		got, err := ParsePlanTier(raw)
		if !errors.Is(err, ErrUnknownPlanTier) || got != PlanTierUnknown {
			t.Errorf("ParsePlanTier(%q) = (%q, %v), want (unknown, ErrUnknownPlanTier)", raw, got, err)
		}
	}
}
