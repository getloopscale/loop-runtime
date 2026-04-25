// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// ScaleProjectStore abstracts project lookup for scale (DIP).
type ScaleProjectStore interface {
	GetByName(ctx context.Context, tenantID, name string) (*ProjectResult, error)
}

// ScaleHandler provides the HTTP handler for scaling operations.
type ScaleHandler struct {
	projects    ScaleProjectStore
	kedaEnabled bool
}

// NewScaleHandler creates a new ScaleHandler.
func NewScaleHandler(projects ScaleProjectStore, kedaEnabled bool) *ScaleHandler {
	return &ScaleHandler{
		projects:    projects,
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

	// Build response — config acknowledged but NOT persisted (stub).
	// The user must also configure autoscaling in loopzero.yaml for the next deploy.
	// TODO: persist to DB + withEnvironmentLock when scaling_config table lands.
	resp := scaleResponse{
		App:         req.App,
		Environment: req.Environment,
		Status:      "acknowledged",
	}

	if hasFixed {
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
		}
		resp.Autoscaling = as
	}

	writeJSON(w, http.StatusAccepted, resp)
}
