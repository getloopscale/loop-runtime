// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// ScaleProjectStore abstracts project lookup for scale (DIP).
type ScaleProjectStore interface {
	GetByName(ctx context.Context, tenantID, name string) (*ProjectResult, error)
}

// ScaleConfigInput is the routes-local mirror of db.ScalingConfig fields the
// handler emits. Defined here to avoid circular import (db package already
// imports routes for attestation_store wiring; routes cannot import db back).
type ScaleConfigInput struct {
	TenantID     string
	ProjectID    string
	Environment  string
	AppName      string
	MinReplicas  int32
	MaxReplicas  int32
	TargetCPUPct *int32
	ScaleToZero  bool
	UpdatedBy    string
}

// ScaleApplyFunc upserts the scaling config, returning (changed, error).
// Bootstrap (api/cmd/setup_orchestrator.go) provides the closure that converts
// ScaleConfigInput to db.ScalingConfig and calls db.PostgresScalingStore.Upsert.
type ScaleApplyFunc func(ctx context.Context, cfg ScaleConfigInput) (changed bool, err error)

// ScaleHandler provides the HTTP handler for scaling operations.
type ScaleHandler struct {
	projects    ScaleProjectStore
	apply       ScaleApplyFunc // optional — when nil, falls back to stub-202 (legacy)
	kedaEnabled bool
}

// NewScaleHandler creates a new ScaleHandler.
//
// `apply` MAY be nil for unit tests that exercise validation paths
// without exercising persistence. Production wiring (api/cmd/setup_orchestrator.go)
// MUST pass a non-nil ScaleApplyFunc — call `EnsureProductionWired` at
// boot to assert this loudly (audit gap M02 #14 fix).
func NewScaleHandler(projects ScaleProjectStore, apply ScaleApplyFunc, kedaEnabled bool) *ScaleHandler {
	return &ScaleHandler{
		projects:    projects,
		apply:       apply,
		kedaEnabled: kedaEnabled,
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
}

// scaleResponse is the JSON response for a successful scale operation.
type scaleResponse struct {
	App         string              `json:"app"`
	Environment string             `json:"environment"`
	Replicas    *int               `json:"replicas,omitempty"`
	Autoscaling *scaleAutoscaling  `json:"autoscaling,omitempty"`
	Status      string             `json:"status"`
}

type scaleAutoscaling struct {
	Min       int  `json:"min"`
	Max       int  `json:"max"`
	CPUTarget int  `json:"cpu_target"`
	Cooldown  *int `json:"cooldown,omitempty"`
}

// Scale handles POST /api/v1/projects/{name}/scale.
//
// Audit gap M02 #14 closure (active-execution-plan §6.C.3): the comment
// that previously described this handler as a "STUB" was outdated —
// since plan T2.2 + B40 (commit `scale-b40-converged-20260506`), the
// handler persists scaling config via the `apply` closure injected by
// `api/cmd/setup_orchestrator.go::buildServerConfig`. The closure
// wraps `db.PostgresScalingStore.Upsert` inside a withEnvironmentLock
// transaction (Rule 6 satisfied).
//
// The `apply == nil` fallback at the end exists ONLY for unit tests in
// `scale_test.go` that exercise validation paths and don't need real
// persistence. Production must wire a non-nil `apply` — see the
// `EnsureProductionWired` panic on misconfiguration below.
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

	// Mutual exclusion: replicas vs autoscaling
	hasFixed := req.Replicas != nil
	hasAuto := req.Min != nil || req.Max != nil || req.CPU != nil
	if hasFixed && hasAuto {
		writeLoopZeroError(w, loopzeroerrors.ErrInvalidRequest.Wrap(
			"cannot mix 'replicas' with 'min'/'max'/'cpu': use 'replicas' for fixed scaling or 'min'/'max'/'cpu' for autoscaling"))
		return
	}
	if !hasFixed && !hasAuto {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap(
			"specify 'replicas' for fixed scaling or 'min'/'max'/'cpu' for autoscaling"))
		return
	}

	// Scale-to-zero gate: min=0 requires KEDA
	if req.Min != nil && *req.Min == 0 && !h.kedaEnabled {
		writeLoopZeroError(w, loopzeroerrors.ErrInvalidRequest.Wrap(
			"scale-to-zero is not available on this cluster. Use min >= 1 for autoscaling, or contact support to enable scale-to-zero."))
		return
	}

	// Verify project exists
	project, err := h.projects.GetByName(r.Context(), uc.TenantID, name)
	if err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if project == nil {
		writeLoopZeroError(w, loopzeroerrors.NewProjectNotFound(name))
		return
	}

	// Build response — defaults computed before persistence.
	resp := scaleResponse{
		App:         req.App,
		Environment: req.Environment,
		Status:      "acknowledged",
	}

	var minR, maxR int32
	var cpuPtr *int32
	scaleToZero := false

	if hasFixed {
		minR = int32(*req.Replicas)
		maxR = int32(*req.Replicas)
		resp.Replicas = req.Replicas
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
		if req.CPU != nil {
			as.CPUTarget = *req.CPU
		} else {
			as.CPUTarget = 80 // default
		}
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
	}

	// Plan T2.2: persist via apply closure when available; otherwise return
	// stub-202 (legacy, transitional). Once all bootstrap call sites pass a
	// real ScaleApplyFunc, this fallback can be removed.
	if h.apply == nil {
		resp.Status = "acknowledged"
		writeJSON(w, http.StatusAccepted, resp)
		return
	}

	cfg := ScaleConfigInput{
		TenantID:     uc.TenantID,
		// B40 (CHG-2026-05-05-DOGFOOD-B40): persist with project NAME, not UUID.
		// CLAUDE.md "Project name vs project ID": CLI sends name as project_id
		// in builds; ScalingResolver consults app_scaling_config using
		// req.ProjectID which IS the name. Storing UUID here breaks the chain.
		ProjectID:    name,
		Environment:  req.Environment,
		AppName:      req.App,
		MinReplicas:  minR,
		MaxReplicas:  maxR,
		TargetCPUPct: cpuPtr,
		ScaleToZero:  scaleToZero,
		UpdatedBy:    uc.UserID,
	}

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

	writeJSON(w, http.StatusOK, resp)
}
