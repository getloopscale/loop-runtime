// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package scalinghttp serves the scaling endpoints under /api/v1/scale: reading and
// changing the replica and resource envelope of a deployed service, bounded by
// the tenant's plan tier.
//
// Eighth family to leave the flat `routes` package (B-080, slice-31), and the
// first whose extraction hit a real symbol of the parent: `ProjectResult`. It
// turned out to be a type ALIAS the parent re-exports from `internal/domain`, so
// the family points at the real type and the coupling never existed — a
// distinction the fence could not have made, because it measures imports and an
// alias hides behind the package that declares it.
//
// Depends on two leaves and no sibling family: `httpio` for the response writers,
// `routestest` for request helpers, and that one only from _test.go.
package scalinghttp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/getloopscale/loopzero/api/internal/httpio"
	"github.com/getloopscale/loopzero/api/internal/plantier"
	"github.com/getloopscale/loopzero/api/internal/ports"

	"github.com/go-chi/chi/v5"

	"github.com/getloopscale/loopzero/api/internal/metrics"
	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	pkgdomain "github.com/getloopscale/loopzero/pkg/domain"
	"github.com/getloopscale/loopzero/pkg/enforcement"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// J-20 Scale plan v1.1 T1.1 absorption — server-authoritative cost preview,
// plan-tier KEDA gate, HPA bounds enforcement, audit log entry.
//
// The handler is the security boundary. Client UI (dashboard) mirrors the
// gates for UX but a Free user bypassing the dashboard via raw API MUST
// land in the same rejection codes here. The `apply` closure (set by
// api/cmd/setup_orchestrator.go) wraps `store.PostgresScalingStore.Upsert`
// inside withEnvironmentLock — concurrent scale calls on the same
// (tenant, project, env) tuple serialize via pg_advisory_xact_lock per
// CLAUDE.md Concurrency Model + EC-5 absorption.

// ScaleProjectStore abstracts project lookup for scale (DIP).
type ScaleProjectStore interface {
	GetByName(ctx context.Context, tenantID, name string) (*ports.ProjectResult, error)
}

// ScaleConfigInput is the routes-local mirror of store.ScalingConfig fields the
// handler emits. Defined here to avoid circular import (db package already
// imports routes for attestation_store wiring; routes cannot import db back).
type ScaleConfigInput struct {
	TenantID        string
	ProjectID       string
	Environment     string
	AppName         string
	MinReplicas     int32
	MaxReplicas     int32
	TargetCPUPct    *int32
	TargetMemoryPct *int32 // J-20 v1.1 T1.3 — HPA memory target
	ScaleToZero     bool
	UpdatedBy       string
}

// ScaleApplyFunc upserts the scaling config, returning (changed, error).
// Bootstrap (api/cmd/setup_orchestrator.go) provides the closure that converts
// ScaleConfigInput to store.ScalingConfig and calls store.PostgresScalingStore.Upsert
// inside withEnvironmentLock (EC-5 lock semantics).
type ScaleApplyFunc func(ctx context.Context, cfg ScaleConfigInput) (changed bool, err error)

// ScaleReplicasChecker mirrors build.PlanCapChecker (cross-package
// to avoid importing services here). Returns (allowed, planName, cap)
// for the given tenant + requested replica count.
//
// Red Team loopzero-cloud#101 absorption (2026-06-27): the scale endpoint
// previously had ZERO max-replica enforcement. A Starter-tier tenant
// could POST replicas:999 and the apply path would honor it (the
// build path enforced via this same checker, but the LIVE scale path
// bypassed it entirely). Wiring this checker here closes the bypass.
type ScaleReplicasChecker interface {
	CheckReplicasValue(ctx context.Context, tenantID string, requested int) (allowed bool, plan string, limit int, err error)
}

// ScaleHandler provides the HTTP handler for scaling operations.
type ScaleHandler struct {
	projects        ScaleProjectStore
	apply           ScaleApplyFunc // optional — when nil, falls back to stub-202 (legacy)
	kedaEnabled     bool
	planTierStore   pkgdomain.PlanTierStore // J-20 T1.1 — resolves tenant's canonical plan
	podCostPerHour  float64                 // J-20 T1.1 EC-2 — server-authoritative cost rate
	replicasChecker ScaleReplicasChecker    // Red Team #101 — max-replica plan cap; nil disables
	// Issue #137 — set by WaiveReplicaCapEnforcement so EnsureProductionWired
	// can tell "deliberately without a cap" from "somebody dropped the wiring".
	capEnforcementWaived bool
	capWaiveReason       string
}

// NewScaleHandler creates a new ScaleHandler.
//
// `apply` MAY be nil for unit tests that exercise validation paths
// without exercising persistence. Production wiring (api/cmd/setup_orchestrator.go)
// MUST pass a non-nil ScaleApplyFunc — call `EnsureProductionWired` at
// boot to assert this loudly (audit gap M02 #14 fix).
//
// Plan tier store + cost rate are wired via setters AFTER construction so
// existing call sites (and tests) compile unchanged. Production wiring at
// setup_orchestrator.go MUST call SetPlanTierStore so the canonical plan
// is resolved instead of fallback-Starter (EC-3 absorption — fail-closed
// against silent tier downgrade).
func NewScaleHandler(projects ScaleProjectStore, apply ScaleApplyFunc, kedaEnabled bool) *ScaleHandler {
	return &ScaleHandler{
		projects:       projects,
		apply:          apply,
		kedaEnabled:    kedaEnabled,
		podCostPerHour: enforcement.DefaultPodCostPerHour,
	}
}

// SetPlanTierStore wires the PlanTierStore used by Scale to resolve the
// tenant's canonical plan tier (matches ProjectHandler.SetPlanTierStore
// canonical from T2.3.b). Production setup MUST call this; tests MAY pass
// nil to exercise validation paths that don't depend on tier resolution.
func (h *ScaleHandler) SetPlanTierStore(s pkgdomain.PlanTierStore) {
	h.planTierStore = s
}

// SetReplicasChecker wires the per-plan replica cap checker used to
// reject /scale requests that exceed the tenant's plan limit. Red Team
// loopzero-cloud#101 (2026-06-27): scale endpoint had ZERO replica cap
// enforcement before this wiring — Starter tier could set replicas:999.
// Production setup at setup_orchestrator.go wires the same
// planLimiterCapAdapter used by the build path so both paths share one
// truth.
func (h *ScaleHandler) SetReplicasChecker(c ScaleReplicasChecker) {
	h.replicasChecker = c
}

// EnsureProductionWired panics if the handler was constructed without a
// non-nil ScaleApplyFunc. Call this from production boot (after wiring
// everything) so a regression that drops the apply closure crashes the
// process loudly at startup instead of silently degrading prod scaling
// to a stub-202 no-op. Tests intentionally pass nil and must not call
// this method.
//
// Audit gap M02 #14: previous code path could regress to stub silently
// if a refactor dropped the apply closure. This guard makes such a
// regression a fail-fast boot panic with a clear message.
func (h *ScaleHandler) EnsureProductionWired() {
	if h.apply == nil {
		panic("ScaleHandler.apply is nil — production must wire a ScaleApplyFunc; scaling would silently no-op (audit M02 #14)")
	}
	// Issue #137 — the same failure shape, on the second thing that can
	// silently vanish. The replica cap is nil-disabled by design, so production
	// depends entirely on setup_orchestrator.go calling SetReplicasChecker.
	// Drop that one line and every tenant may request any replica count with no
	// error anywhere: the cap does not fail, it evaporates.
	//
	// Enforcement genuinely cannot run in some boots — with no database there is
	// no PlanLimiter and therefore no plan to enforce. So the contract is not
	// "the checker must exist"; it is "the caller must have DECIDED". Wire the
	// checker, or waive it out loud. Silence is the only thing forbidden.
	if h.replicasChecker == nil && !h.capEnforcementWaived {
		panic("ScaleHandler.replicasChecker is nil and enforcement was not explicitly waived — " +
			"call SetReplicasChecker, or WaiveReplicaCapEnforcement(reason) if this boot genuinely " +
			"cannot enforce; otherwise the per-plan replica cap disables silently (issue #137)")
	}
}

// WaiveReplicaCapEnforcement records that this handler runs WITHOUT the
// per-plan replica cap, and why. It exists so that "no enforcement" is a
// statement someone made rather than a line someone forgot.
//
// The legitimate case is a boot with no database: no PlanLimiter can be built,
// so there is no plan to enforce. Reaching for this to silence the guard in a
// boot that could enforce is the regression the guard exists to catch.
func (h *ScaleHandler) WaiveReplicaCapEnforcement(reason string) {
	h.capEnforcementWaived = true
	h.capWaiveReason = reason
}

// scaleRequest is the JSON body for POST /api/v1/projects/{name}/scale.
type scaleRequest struct {
	Environment string `json:"environment"`
	App         string `json:"app"`
	Replicas    *int   `json:"replicas,omitempty"`
	Min         *int   `json:"min,omitempty"`
	Max         *int   `json:"max,omitempty"`
	CPU         *int   `json:"cpu,omitempty"`
	Memory      *int   `json:"memory,omitempty"` // J-20 v1.1 T1.3 — HPA memory target
}

// scaleResponse is the JSON response for a successful scale operation.
type scaleResponse struct {
	App            string            `json:"app"`
	Environment    string            `json:"environment"`
	Replicas       *int              `json:"replicas,omitempty"`
	Autoscaling    *scaleAutoscaling `json:"autoscaling,omitempty"`
	Status         string            `json:"status"`
	MonthlyCostUSD float64           `json:"monthly_cost_usd"` // J-20 v1.1 T1.1 EC-2 — server-authoritative
	PlanTier       string            `json:"plan_tier"`        // J-20 v1.1 — surfaces resolved tier for dashboard CTA
}

type scaleAutoscaling struct {
	Min          int  `json:"min"`
	Max          int  `json:"max"`
	CPUTarget    int  `json:"cpu_target"`
	MemoryTarget int  `json:"memory_target"` // J-20 v1.1 T1.3 — HPA memory target
	Cooldown     *int `json:"cooldown,omitempty"`
}

// Scale handles POST /api/v1/projects/{name}/scale.
//
// Plan v1.1 T1.1 wired closes EC-2 (server-authoritative cost), EC-3 (KEDA
// plan-tier gate), EC-4 (HPA bounds), EC-5 (lock via apply closure), and
// the audit log entry at the success path.
//
// Plan 2026-06-15-code-review-49-findings-remediation T3.1 (R2-F2 HIGH):
// previous monolith was CC=37. Per ADR D4 decomposed into 5 helpers
// (parseScaleRequest + resolveTier + enforceLimits + buildScaleResult +
// applyAndLog) — orchestrator below ≤ CC 12 + each helper ≤ CC 12.
func (h *ScaleHandler) Scale(w http.ResponseWriter, r *http.Request) {
	uc := auth.GetUserContext(r.Context())
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("project name is required"))
		return
	}

	req, hasFixed, hasAuto, perr := parseScaleRequest(r)
	if perr != nil {
		httpio.WriteLoopZeroError(w, perr)
		return
	}

	planTier, terr := plantier.Resolve(r.Context(), h.planTierStore, uc.TenantID)
	if terr != nil {
		httpio.WriteLoopZeroError(w, terr)
		return
	}

	if lerr := h.enforceLimits(r.Context(), &req, planTier, hasAuto, uc.TenantID); lerr != nil {
		httpio.WriteLoopZeroError(w, lerr)
		return
	}

	// Verify project exists (cross-tenant safety: GetByName scoped to tenant).
	project, gerr := h.projects.GetByName(r.Context(), uc.TenantID, name)
	if gerr != nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if project == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.NewProjectNotFound(name))
		return
	}

	decision := h.buildScaleResult(&req, hasFixed, planTier, uc.TenantID, name, uc.UserID)

	// Plan T2.2 fallback: when no apply closure wired (tests), return stub
	// 202 with the computed preview so callers still see canonical shape.
	if h.apply == nil {
		httpio.WriteJSON(w, http.StatusAccepted, decision.resp)
		return
	}

	h.applyAndLog(w, r, &decision)
}

// parseScaleRequest decodes the request body and validates its shape. It
// returns the request (environment defaulted to production), which of the two
// modes it asks for, and the LoopZeroError to write when it is refused.
func parseScaleRequest(r *http.Request) (req scaleRequest, hasFixed, hasAuto bool, err *loopzeroerrors.LoopZeroError) {
	if decErr := json.NewDecoder(r.Body).Decode(&req); decErr != nil {
		return req, false, false, loopzeroerrors.ErrInvalidRequest.Wrap("invalid JSON body")
	}
	if req.Environment == "" {
		req.Environment = "production"
	}
	hasFixed, hasAuto = req.mode()
	return req, hasFixed, hasAuto, req.validate(hasFixed, hasAuto)
}

// mode reports whether the request asks for fixed replicas, autoscaling, or
// (invalidly) both or neither.
func (req *scaleRequest) mode() (fixed, auto bool) {
	fixed = req.Replicas != nil
	auto = req.Min != nil || req.Max != nil || req.CPU != nil || req.Memory != nil
	return fixed, auto
}

// validate returns the first reason the request is refused, or nil.
func (req *scaleRequest) validate(fixed, auto bool) *loopzeroerrors.LoopZeroError {
	if req.App == "" {
		return loopzeroerrors.ErrMissingField.Wrap("app name is required")
	}
	if err := validateScaleMode(fixed, auto); err != nil {
		return err
	}
	return req.validateReplicaBounds(fixed)
}

// validateScaleMode requires exactly one of the two modes.
func validateScaleMode(fixed, auto bool) *loopzeroerrors.LoopZeroError {
	switch {
	case fixed && auto:
		return loopzeroerrors.ErrInvalidRequest.Wrap(
			"cannot mix 'replicas' with 'min'/'max'/'cpu'/'memory': use 'replicas' for fixed scaling or autoscaling fields for autoscaling")
	case !fixed && !auto:
		return loopzeroerrors.ErrMissingField.Wrap(
			"specify 'replicas' for fixed scaling or 'min'/'max'/'cpu'/'memory' for autoscaling")
	}
	return nil
}

// validateReplicaBounds refuses replica counts outside what the mode allows.
// Red Team loopzero-cloud#103: these are client input errors, answered as 400
// INVALID_REPLICAS, never as a 5xx.
func (req *scaleRequest) validateReplicaBounds(fixed bool) *loopzeroerrors.LoopZeroError {
	switch {
	case fixed && *req.Replicas <= 0:
		return invalidReplicas("replicas must be > 0 (use 0 only via 'min: 0' on autoscaling, not 'replicas')")
	case req.Min != nil && *req.Min < 0:
		return invalidReplicas("autoscaling 'min' must be >= 0")
	case req.Max != nil && *req.Max <= 0:
		return invalidReplicas("autoscaling 'max' must be > 0")
	}
	return nil
}

func invalidReplicas(message string) *loopzeroerrors.LoopZeroError {
	return loopzeroerrors.NewLoopZeroError("INVALID_REPLICAS", message)
}

// enforceLimits applies the EC-3 KEDA tier gate + EC-4 HPA bounds. Server-
// side enforcement at the handler boundary; client UI mirrors for UX.
// Returns the first LoopZeroError to write, or nil when both gates pass.
func (h *ScaleHandler) enforceLimits(
	ctx context.Context,
	req *scaleRequest,
	planTier pkgdomain.PlanTier,
	hasAuto bool,
	tenantID string,
) *loopzeroerrors.LoopZeroError {
	if err := h.enforceKEDA(req, planTier); err != nil {
		return err
	}
	// Red Team loopzero-cloud#101 absorption (2026-06-27): enforce the
	// per-plan max-replicas cap on EVERY mutation path, not just the
	// build path. The cap is the same one services.enforceScaling
	// already uses for loopzero deploy; wiring it here covers loopzero scale
	// + the dashboard scale form + any direct API client. Without
	// this, Starter tier could set replicas:999 and the apply path
	// honored it.
	if err := h.enforceReplicaCap(ctx, req, tenantID); err != nil {
		return err
	}
	if !hasAuto {
		return nil
	}
	return enforceHPABounds(req)
}

// enforceReplicaCap rejects a scale request whose effective replica
// count exceeds the tenant's plan cap. For fixed scaling, the cap
// gates `replicas`. For autoscaling, the cap gates `max` (the
// ceiling — what the cluster would actually scale TO under load).
// A nil checker disables enforcement (tests + legacy callsites). A cap that
// cannot be read refuses the request: the checker fails closed, and an
// unknown cap is never treated as no cap.
func (h *ScaleHandler) enforceReplicaCap(ctx context.Context, req *scaleRequest, tenantID string) *loopzeroerrors.LoopZeroError {
	if h.replicasChecker == nil {
		return nil
	}
	var requested int
	switch {
	case req.Replicas != nil && *req.Replicas > 0:
		requested = *req.Replicas
	case req.Max != nil && *req.Max > 0:
		requested = *req.Max
	default:
		return nil
	}
	allowed, plan, cap, err := h.replicasChecker.CheckReplicasValue(ctx, tenantID, requested)
	if err != nil {
		slog.Error("scale: replica cap cannot be read; refusing the request",
			"tenant_id", tenantID, "app", req.App, "requested", requested, "error", err)
		var te *loopzeroerrors.LoopZeroError
		if !errors.As(err, &te) {
			te = loopzeroerrors.ErrPlatformDegraded
		}
		return te
	}
	if allowed {
		return nil
	}
	metrics.PlanLimitViolations.WithLabelValues("max_replicas_per_service", plan).Inc()
	return loopzeroerrors.NewReplicasExceedPlan(plan, cap, requested, req.App)
}

// enforceKEDA implements EC-3: scale-to-zero (min == 0) requires plan
// tier ≥ Pro AND cluster KEDA enabled. Each check returns its own typed
// LoopZeroError so callers map to canonical HTTP status codes.
func (h *ScaleHandler) enforceKEDA(req *scaleRequest, planTier pkgdomain.PlanTier) *loopzeroerrors.LoopZeroError {
	if req.Min == nil || *req.Min != 0 {
		return nil
	}
	if err := enforcement.AssertKEDAAllowed(string(planTier)); err != nil {
		return loopzeroerrors.NewPlanUpgradeRequired("pro", string(planTier), "KEDA scale-to-zero")
	}
	if !h.kedaEnabled {
		return loopzeroerrors.ErrInvalidRequest.Wrap(
			"scale-to-zero is not available on this cluster. Use min >= 1 for autoscaling, or contact support to enable scale-to-zero.")
	}
	return nil
}

// enforceHPABounds implements EC-4: cpu ∈ [10,95], memory ∈ [10,90]. Only
// active when the user supplied at least one of cpu / memory; defaults
// fall within the valid range and never trigger this gate.
func enforceHPABounds(req *scaleRequest) *loopzeroerrors.LoopZeroError {
	if req.CPU != nil {
		if err := assertHPABounds(*req.CPU, valueOr(req.Memory, enforcement.DefaultHPAMemoryTarget)); err != nil {
			return err
		}
	}
	if req.Memory != nil && req.CPU == nil {
		// CPU defaulted later; validate memory in isolation against its
		// own bounds (cpu uses default 80 which is always valid).
		if err := assertHPABounds(valueOr(req.CPU, enforcement.DefaultHPACPUTarget), *req.Memory); err != nil {
			return err
		}
	}
	return nil
}

// assertHPABounds wraps enforcement.AssertHPATargetBounds + maps a typed
// HPATargetBoundsError to the canonical loopzeroerrors factory (400). Generic
// enforcement errors degrade to ErrInvalidRequest.
func assertHPABounds(cpu, memory int) *loopzeroerrors.LoopZeroError {
	err := enforcement.AssertHPATargetBounds(cpu, memory)
	if err == nil {
		return nil
	}
	var be *enforcement.HPATargetBoundsError
	if errors.As(err, &be) {
		return loopzeroerrors.NewHPATargetBoundsViolated(be.Field, be.Min, be.Max, be.Got)
	}
	return loopzeroerrors.ErrInvalidRequest.Wrap(err.Error())
}

// scaleDecision is what a validated scale request resolves to: the config to
// persist, the response to answer with, and the facts the audit line records.
type scaleDecision struct {
	cfg               ScaleConfigInput
	resp              scaleResponse
	planTier          pkgdomain.PlanTier
	effectiveReplicas int  // replicas the cost is computed for (max, for autoscaling)
	scaleToZero       bool // min is 0 (KEDA wiring at apply time)
}

// buildScaleResult composes the scaleDecision for both fixed-replicas and
// autoscaling modes.
func (h *ScaleHandler) buildScaleResult(
	req *scaleRequest,
	hasFixed bool,
	planTier pkgdomain.PlanTier,
	tenantID, projectName, userID string,
) scaleDecision {
	var effectiveReplicas int
	var scaleToZero bool
	resp := scaleResponse{
		App:         req.App,
		Environment: req.Environment,
		Status:      "acknowledged",
		PlanTier:    string(planTier),
	}

	var minR, maxR int32
	var cpuPtr, memPtr *int32

	if hasFixed {
		minR = int32(*req.Replicas)
		maxR = int32(*req.Replicas)
		resp.Replicas = req.Replicas
		effectiveReplicas = *req.Replicas
	} else {
		as := buildAutoscaling(req)
		resp.Autoscaling = as
		minR = int32(as.Min)
		maxR = int32(as.Max)
		c := int32(as.CPUTarget)
		cpuPtr = &c
		m := int32(as.MemoryTarget)
		memPtr = &m
		scaleToZero = as.Min == 0
		// Cost preview for HPA mode uses Max replicas as the conservative
		// estimate — the dashboard surfaces "up to $X/mo" copy per ADR D1.
		effectiveReplicas = as.Max
	}

	// EC-2 absorption: server-authoritative monthly cost.
	resp.MonthlyCostUSD = enforcement.ComputeMonthlyCost(effectiveReplicas, h.podCostPerHour)

	cfg := ScaleConfigInput{
		TenantID: tenantID,
		// B40 (CHG-2026-05-05-DOGFOOD-B40): persist with project NAME, not UUID.
		// CLAUDE.md "Project name vs project ID": CLI sends name as project_id
		// in builds; ScalingResolver consults app_scaling_config using
		// req.ProjectID which IS the name. Storing UUID here breaks the chain.
		ProjectID:       projectName,
		Environment:     req.Environment,
		AppName:         req.App,
		MinReplicas:     minR,
		MaxReplicas:     maxR,
		TargetCPUPct:    cpuPtr,
		TargetMemoryPct: memPtr,
		ScaleToZero:     scaleToZero,
		UpdatedBy:       userID,
	}
	return scaleDecision{
		cfg:               cfg,
		resp:              resp,
		planTier:          planTier,
		effectiveReplicas: effectiveReplicas,
		scaleToZero:       scaleToZero,
	}
}

// buildAutoscaling computes the canonical scaleAutoscaling block, filling
// what the request left out with the enforcement defaults (max replicas, CPU
// and memory targets, and the scale-to-zero cooldown when min is 0).
func buildAutoscaling(req *scaleRequest) *scaleAutoscaling {
	as := &scaleAutoscaling{
		Max:          valueOr(req.Max, enforcement.DefaultHPAMaxReplicas),
		CPUTarget:    valueOr(req.CPU, enforcement.DefaultHPACPUTarget),
		MemoryTarget: valueOr(req.Memory, enforcement.DefaultHPAMemoryTarget),
	}
	if req.Min != nil {
		as.Min = *req.Min
	}
	if as.Min == 0 {
		cooldown := enforcement.DefaultScaleToZeroCooldownSeconds
		as.Cooldown = &cooldown
	}
	return as
}

// applyAndLog invokes the apply closure (EC-5: wraps withEnvironmentLock
// around the Upsert via pg_advisory_xact_lock per Concurrency Model),
// maps errors to LoopZeroError, sets the final status, and writes the audit
// slog entry for the metrics pipeline.
func (h *ScaleHandler) applyAndLog(w http.ResponseWriter, r *http.Request, d *scaleDecision) {
	changed, err := h.apply(r.Context(), d.cfg)
	if err != nil {
		var te *loopzeroerrors.LoopZeroError
		if errors.As(err, &te) {
			httpio.WriteLoopZeroError(w, te)
			return
		}
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if changed {
		d.resp.Status = "applied"
	} else {
		d.resp.Status = "no_change"
	}
	slog.Info("scale.applied",
		"tenant_id", d.cfg.TenantID,
		"project", d.cfg.ProjectID,
		"environment", d.cfg.Environment,
		"app", d.cfg.AppName,
		"plan_tier", string(d.planTier),
		"replicas", d.effectiveReplicas,
		"scale_to_zero", d.scaleToZero,
		"monthly_cost_usd", d.resp.MonthlyCostUSD,
		"changed", changed,
	)
	httpio.WriteJSON(w, http.StatusOK, d.resp)
}

// valueOr returns *p, or fallback when p is nil.
func valueOr(p *int, fallback int) int {
	if p == nil {
		return fallback
	}
	return *p
}
