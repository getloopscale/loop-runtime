// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package domain

import (
	"context"
	"errors"
)

// PlanTier is the canonical LoopRuntime subscription-plan identifier per ADR D4
// (decisions/2026-06-11-plan-tier-canonical-resolution.md).
//
// Replaces the unconstrained string literals (`"starter"`, `"pro"`, ...)
// that appear at 8+ call sites across the billing, enforcement, and
// tenant-provisioning paths. Closes H7 CRITICAL from /loop-code-review:
// projects.go:233 hardcoded `Plan: "starter"` in EnsureTenant which
// silently downgraded paying tenants on Connect.
//
// "growth" was the pre-2026-05-12 tier name (legacy CRD enum). It is
// NOT part of the canonical set; ResolvePlanTier maps any database row
// with Plan="growth" to PlanTierPro (the closest functional equivalent
// in the post-MEET-2026-05-12 4-tier pricing). This alias path is
// auditable + reversible: the row in the plans table is the source of
// truth, the mapping happens at read time.
type PlanTier string

const (
	// PlanTierUnknown is the zero value — returned together with
	// ErrUnknownPlanTier when a DB row exists but the Plan value is
	// not in the recognized set. Callers MUST treat this as an error
	// path: NEVER default to PlanTierStarter on Unknown — that would
	// reintroduce the silent downgrade pattern H7 closes.
	PlanTierUnknown PlanTier = ""

	// PlanTierStarter is the free tier (3 projects).
	// Brand-new tenants with NO plans row receive this value with nil
	// error — the only documented fallback path.
	PlanTierStarter PlanTier = "starter"

	// PlanTierPro is the indie tier ($29/mo, 10 projects).
	// Canonical post-MEET-2026-05-12 pricing.
	PlanTierPro PlanTier = "pro"

	// PlanTierTeam is the team tier ($99/mo, unlimited projects + RBAC
	// + progressive delivery config). Early Access.
	PlanTierTeam PlanTier = "team"

	// PlanTierEnterprise is the sales-led tier (custom pricing,
	// multi-cell + SSO/SAML).
	PlanTierEnterprise PlanTier = "enterprise"

	// legacyPlanTierGrowth is the pre-2026-05-12 2-tier-pricing label
	// that still appears in older CRDs and tenant rows. Mapped to Pro
	// at read time — see ResolvePlanTier.
	legacyPlanTierGrowth = "growth"
)

// ErrUnknownPlanTier is returned together with PlanTierUnknown when the
// database row's Plan column contains an unrecognized value (future tier
// added to DB but not yet to enum, manual DB edit, data corruption).
// Callers MUST handle this — typically 503 + audit log + ops alert.
// Silent downgrade is forbidden by the contract (ADR D4).
var ErrUnknownPlanTier = errors.New("unknown plan tier value in database")

// PlanTierStore is the minimal contract ResolvePlanTier consumes. The
// concrete pgx-backed implementation lives next to other stores.
type PlanTierStore interface {
	// GetTenantPlan returns the raw Plan column value for the tenant.
	// Implementations MUST return ("", nil) when the row is absent (the
	// brand-new tenant onboarding path). All other not-found shapes
	// (transport errors, partial reads) must surface a non-nil error.
	GetTenantPlan(ctx context.Context, tenantID string) (string, error)
}

// ResolvePlanTier resolves a tenant's canonical PlanTier from a store
// per ADR D4. Returns:
//   - (PlanTierStarter, nil) — brand-new tenant with NO row (onboarding
//     fallback ONLY).
//   - (<resolved tier>, nil) — row exists with a recognized value, OR
//     with legacy "growth" (mapped to PlanTierPro per ADR D4).
//   - (PlanTierUnknown, ErrUnknownPlanTier) — row exists but Plan value
//     is unrecognized. NEVER defaults to PlanTierStarter (H7 closure).
//   - (PlanTierUnknown, <wrapped store-error>) — transport failure.
//
// Callers MUST handle ErrUnknownPlanTier. Defaulting to Starter on this
// path silently downgrades paying tenants — the very bug H7 reports.
func ResolvePlanTier(ctx context.Context, store PlanTierStore, tenantID string) (PlanTier, error) {
	if store == nil {
		return PlanTierUnknown, errors.New("ResolvePlanTier: nil store")
	}
	if tenantID == "" {
		return PlanTierUnknown, errors.New("ResolvePlanTier: empty tenantID")
	}
	raw, err := store.GetTenantPlan(ctx, tenantID)
	if err != nil {
		return PlanTierUnknown, err
	}
	if raw == "" {
		// Brand-new tenant — onboarding fallback. The ONLY documented
		// path that defaults to Starter without an error.
		return PlanTierStarter, nil
	}
	return ParsePlanTier(raw)
}

// ParsePlanTier converts a stored or configured plan value into the canonical
// PlanTier. It is the one place that knows the recognised vocabulary,
// including the legacy "growth" alias (mapped to PlanTierPro per ADR D4).
//
// An unrecognised value — including the empty string — returns
// (PlanTierUnknown, ErrUnknownPlanTier). It NEVER defaults to Starter: the
// only Starter fallback is ResolvePlanTier's "no row yet" onboarding path,
// which it decides before calling this.
func ParsePlanTier(raw string) (PlanTier, error) {
	switch raw {
	case string(PlanTierStarter),
		string(PlanTierPro),
		string(PlanTierTeam),
		string(PlanTierEnterprise):
		return PlanTier(raw), nil
	case legacyPlanTierGrowth:
		// Backward-compat alias for pre-MEET-2026-05-12 pricing.
		// Existing "growth" tenants are functionally Pro tier.
		return PlanTierPro, nil
	default:
		return PlanTierUnknown, ErrUnknownPlanTier
	}
}

// IsValid reports whether a PlanTier value is one of the canonical
// recognized set. PlanTierUnknown returns false (it is the sentinel for
// "could not resolve", not a valid tier).
func (p PlanTier) IsValid() bool {
	switch p {
	case PlanTierStarter, PlanTierPro, PlanTierTeam, PlanTierEnterprise:
		return true
	default:
		return false
	}
}

// DisplayName returns the product name of the tier as shown to users
// ("Starter", "Pro", "Team", "Enterprise"). An unrecognised value is returned
// as-is rather than invented, so an unexpected tier stays visible.
func (p PlanTier) DisplayName() string {
	switch p {
	case PlanTierStarter:
		return "Starter"
	case PlanTierPro:
		return "Pro"
	case PlanTierTeam:
		return "Team"
	case PlanTierEnterprise:
		return "Enterprise"
	default:
		return string(p)
	}
}

// String returns the wire-format string used in DB columns, CRDs, and
// JSON responses. PlanTierUnknown returns the empty string by design —
// JSON marshallers + database drivers preserve that as NULL where
// appropriate.
func (p PlanTier) String() string {
	return string(p)
}
