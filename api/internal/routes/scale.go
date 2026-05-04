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
// Plan T2.2: `apply` parameter may be nil during transition (stub-202 fallback).
// Production callers MUST pass a real ScaleApplyFunc for persistence.
func NewScaleHandler(projects ScaleProjectStore, apply ScaleApplyFunc, kedaEnabled bool) *ScaleHandler {
	return &ScaleHandler{
		projects:    projects,
		apply:       apply,
		kedaEnabled: kedaEnabled,
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
// STUB (pre-release): validates the scaling config and returns 202 Accepted with
// status "acknowledged". Config is NOT persisted to DB — the user must also set
// autoscaling in loopzero.yaml, and the config takes effect on the next `loopzero deploy`.
//
// When DB persistence lands (environment_targets or scaling_config table), this
// handler MUST acquire withEnvironmentLock (Rule 6) before writing.
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
		ProjectID:    project.ID,
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
