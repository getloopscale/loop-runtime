// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/middleware"
	pkgdomain "github.com/loopzero/loopzero/pkg/domain"
	"github.com/loopzero/loopzero/pkg/enforcement"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// J-20 Scale plan v1.1 T1.1 absorption — server-authoritative cost preview,
// plan-tier KEDA gate, HPA bounds enforcement, audit log entry.
//
// The handler is the security boundary. Client UI (dashboard) mirrors the
// gates for UX but a Free user bypassing the dashboard via raw API MUST
// land in the same rejection codes here. The `apply` closure (set by
// api/cmd/setup_orchestrator.go) wraps `db.PostgresScalingStore.Upsert`
// inside withEnvironmentLock — concurrent scale calls on the same
// (tenant, project, env) tuple serialize via pg_advisory_xact_lock per
// CLAUDE.md Concurrency Model + EC-5 absorption.

// DefaultPodCostPerHour is the conservative per-pod hourly cost used when
// no per-cluster rate has been wired. Matches the public pricing page at
// https://loopzero.example/pricing as of plan v1.1 sign-off.
//
// Source of truth: dashboard cost preview MUST reconcile to whatever value
// the handler returns in `monthly_cost_usd` — client preview is hint, not
// authority (ADR D1 + EC-2).
const DefaultPodCostPerHour float64 = 0.05

// ScaleProjectStore abstracts project lookup for scale (DIP).
type ScaleProjectStore interface {
	GetByName(ctx context.Context, tenantID, name string) (*ProjectResult, error)
}

// ScaleConfigInput is the routes-local mirror of db.ScalingConfig fields the
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
// ScaleConfigInput to db.ScalingConfig and calls db.PostgresScalingStore.Upsert
// inside withEnvironmentLock (EC-5 lock semantics).
type ScaleApplyFunc func(ctx context.Context, cfg ScaleConfigInput) (changed bool, err error)

// ScaleHandler provides the HTTP handler for scaling operations.
type ScaleHandler struct {
	projects        ScaleProjectStore
	apply           ScaleApplyFunc // optional — when nil, falls back to stub-202 (legacy)
	kedaEnabled     bool
	planTierStore   pkgdomain.PlanTierStore // J-20 T1.1 — resolves tenant's canonical plan
	podCostPerHour  float64                 // J-20 T1.1 EC-2 — server-authoritative cost rate
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
		podCostPerHour: DefaultPodCostPerHour,
	}
}

// SetPlanTierStore wires the PlanTierStore used by Scale to resolve the
// tenant's canonical plan tier (matches ProjectHandler.SetPlanTierStore
// canonical from T2.3.b). Production setup MUST call this; tests MAY pass
// nil to exercise validation paths that don't depend on tier resolution.
func (h *ScaleHandler) SetPlanTierStore(s pkgdomain.PlanTierStore) {
	h.planTierStore = s
}

// SetPodCostPerHour overrides the default per-pod hourly cost (USD). Tests
// + per-cluster pricing tuning use this to inject deterministic values.
// Non-positive values fall back to DefaultPodCostPerHour (defensive).
func (h *ScaleHandler) SetPodCostPerHour(usd float64) {
	if usd > 0 {
		h.podCostPerHour = usd
	}
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
func (h *ScaleHandler) Scale(w http.ResponseWriter, r *http.Request) {
	uc := middleware.GetUserContext(r.Context())
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("project name is required"))
		return
	}

	var req scaleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInvalidRequest.Wrap("invalid JSON body"))
		return
	}

	if req.App == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("app name is required"))
		return
	}
	if req.Environment == "" {
		req.Environment = "production"
	}

	// Mutual exclusion: replicas vs autoscaling per ADR D2.
	hasFixed := req.Replicas != nil
	hasAuto := req.Min != nil || req.Max != nil || req.CPU != nil || req.Memory != nil
	if hasFixed && hasAuto {
		writeLoopZeroError(w, loopzeroerrors.ErrInvalidRequest.Wrap(
			"cannot mix 'replicas' with 'min'/'max'/'cpu'/'memory': use 'replicas' for fixed scaling or autoscaling fields for autoscaling"))
		return
	}
	if !hasFixed && !hasAuto {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap(
			"specify 'replicas' for fixed scaling or 'min'/'max'/'cpu'/'memory' for autoscaling"))
		return
	}

	// Resolve canonical plan tier per ADR D4 — fail-closed on store error so a
	// transient DB hiccup does not silently grant Free user the KEDA path.
	planTier := pkgdomain.PlanTierStarter
	if h.planTierStore != nil {
		resolved, perr := pkgdomain.ResolvePlanTier(r.Context(), h.planTierStore, uc.TenantID)
		if perr != nil {
			slog.Error("scale: failed to resolve canonical plan tier",
				"tenant_id", uc.TenantID, "error", perr.Error())
			writeLoopZeroError(w, loopzeroerrors.ErrPlatformDegraded.Wrap(
				"plan tier resolution failed — retry or contact support"))
			return
		}
		planTier = resolved
	}

	// EC-3 absorption: KEDA scale-to-zero requires plan tier ≥ Pro.
	// Server-side enforcement at handler boundary; client UI mirrors for UX.
	if req.Min != nil && *req.Min == 0 {
		if err := enforcement.AssertKEDAAllowed(string(planTier)); err != nil {
			writeLoopZeroError(w, loopzeroerrors.NewPlanUpgradeRequired(
				"pro", string(planTier), "KEDA scale-to-zero"))
			return
		}
		// Cluster KEDA installed check (independent of plan tier — a Pro user
		// on a cluster without KEDA still cannot scale to zero).
		if !h.kedaEnabled {
			writeLoopZeroError(w, loopzeroerrors.ErrInvalidRequest.Wrap(
				"scale-to-zero is not available on this cluster. Use min >= 1 for autoscaling, or contact support to enable scale-to-zero."))
			return
		}
	}

	// EC-4 absorption: HPA target bounds. cpu ∈ [10,95], memory ∈ [10,90].
	// Enforced only when HPA mode + the target field is provided. Defaults
	// applied below fall within the valid range.
	if hasAuto {
		if req.CPU != nil {
			if err := enforcement.AssertHPATargetBounds(*req.CPU, defaultMemoryTarget(req.Memory)); err != nil {
				var be *enforcement.HPATargetBoundsError
				if errors.As(err, &be) {
					writeLoopZeroError(w, loopzeroerrors.NewHPATargetBoundsViolated(
						be.Field, be.Min, be.Max, be.Got))
					return
				}
				writeLoopZeroError(w, loopzeroerrors.ErrInvalidRequest.Wrap(err.Error()))
				return
			}
		}
		if req.Memory != nil && req.CPU == nil {
			// CPU defaulted later; validate memory in isolation against its
			// own bounds (cpu uses default 80 which is always valid).
			if err := enforcement.AssertHPATargetBounds(defaultCPUTarget(req.CPU), *req.Memory); err != nil {
				var be *enforcement.HPATargetBoundsError
				if errors.As(err, &be) {
					writeLoopZeroError(w, loopzeroerrors.NewHPATargetBoundsViolated(
						be.Field, be.Min, be.Max, be.Got))
					return
				}
				writeLoopZeroError(w, loopzeroerrors.ErrInvalidRequest.Wrap(err.Error()))
				return
			}
		}
	}

	// Verify project exists (cross-tenant safety: GetByName scoped to tenant).
	project, err := h.projects.GetByName(r.Context(), uc.TenantID, name)
	if err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if project == nil {
		writeLoopZeroError(w, loopzeroerrors.NewProjectNotFound(name))
		return
	}

	// Build response — defaults computed before persistence so the canonical
	// values flow back to the dashboard for hint/preview reconciliation.
	resp := scaleResponse{
		App:         req.App,
		Environment: req.Environment,
		Status:      "acknowledged",
		PlanTier:    string(planTier),
	}

	var minR, maxR int32
	var cpuPtr *int32
	var memPtr *int32
	scaleToZero := false
	var effectiveReplicas int

	if hasFixed {
		minR = int32(*req.Replicas)
		maxR = int32(*req.Replicas)
		resp.Replicas = req.Replicas
		effectiveReplicas = *req.Replicas
	} else {
		as := &scaleAutoscaling{}
		if req.Min != nil {
			as.Min = *req.Min
		}
		if req.Max != nil {
			as.Max = *req.Max
		} else {
			as.Max = 3 // default
		}
		as.CPUTarget = defaultCPUTarget(req.CPU)
		as.MemoryTarget = defaultMemoryTarget(req.Memory)
		if as.Min == 0 {
			defaultCooldown := 300
			as.Cooldown = &defaultCooldown
			scaleToZero = true
		}
		resp.Autoscaling = as
		minR = int32(as.Min)
		maxR = int32(as.Max)
		c := int32(as.CPUTarget)
		cpuPtr = &c
		m := int32(as.MemoryTarget)
		memPtr = &m
		// Cost preview for HPA mode uses Max replicas as the conservative
		// estimate — the dashboard surfaces "up to $X/mo" copy per ADR D1.
		effectiveReplicas = as.Max
	}

	// EC-2 absorption: server-authoritative monthly cost. Replaces any
	// client-side preview at confirmation time — dashboard MUST display
	// this value (not the local estimate).
	resp.MonthlyCostUSD = enforcement.ComputeMonthlyCost(effectiveReplicas, h.podCostPerHour)

	// Plan T2.2 fallback: when no apply closure wired (tests), return stub
	// 202 with the computed preview so callers still see canonical shape.
	if h.apply == nil {
		resp.Status = "acknowledged"
		writeJSON(w, http.StatusAccepted, resp)
		return
	}

	cfg := ScaleConfigInput{
		TenantID: uc.TenantID,
		// B40 (CHG-2026-05-05-DOGFOOD-B40): persist with project NAME, not UUID.
		// CLAUDE.md "Project name vs project ID": CLI sends name as project_id
		// in builds; ScalingResolver consults app_scaling_config using
		// req.ProjectID which IS the name. Storing UUID here breaks the chain.
		ProjectID:       name,
		Environment:     req.Environment,
		AppName:         req.App,
		MinReplicas:     minR,
		MaxReplicas:     maxR,
		TargetCPUPct:    cpuPtr,
		TargetMemoryPct: memPtr,
		ScaleToZero:     scaleToZero,
		UpdatedBy:       uc.UserID,
	}

	// EC-5 absorption: the apply closure wraps withEnvironmentLock around
	// the Upsert (pg_advisory_xact_lock on tenant+project+env) per
	// CLAUDE.md Concurrency Model. Concurrent scale calls serialize here.
	changed, err := h.apply(r.Context(), cfg)
	if err != nil {
		var te *loopzeroerrors.LoopZeroError
		if errors.As(err, &te) {
			writeLoopZeroError(w, te)
			return
		}
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	if changed {
		resp.Status = "applied"
	} else {
		resp.Status = "no_change"
	}

	// Audit trail — structured log entry (audit_log row added when an
	// auditStore is wired post-T1.1; the slog entry is sufficient for the
	// metrics pipeline `loopzero_scale_enforcement_total{outcome,tier}` per
	// plan v1.1 § Observability).
	slog.Info("scale.applied",
		"tenant_id", uc.TenantID,
		"project", name,
		"environment", req.Environment,
		"app", req.App,
		"plan_tier", string(planTier),
		"replicas", effectiveReplicas,
		"scale_to_zero", scaleToZero,
		"monthly_cost_usd", resp.MonthlyCostUSD,
		"changed", changed,
	)

	writeJSON(w, http.StatusOK, resp)
}

// defaultCPUTarget returns 80 (canonical Kubernetes HPA default) when no
// explicit target was provided. Pinned at module level so the test suite
// + handler share one source of truth.
func defaultCPUTarget(p *int) int {
	if p == nil {
		return 80
	}
	return *p
}

// defaultMemoryTarget returns 80 when no explicit memory target was
// provided. Within the canonical [10,90] memory bounds — never triggers
// the validation rejection on its own.
func defaultMemoryTarget(p *int) int {
	if p == nil {
		return 80
	}
	return *p
}
