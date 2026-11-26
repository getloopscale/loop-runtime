// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package rollbackhttp

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/getloopscale/loop-runtime/api/internal/httpio"
	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// RollbackEntry is one recorded rollback as the store holds it. Status is the
// stored value: IN_PROGRESS, SUCCEEDED or FAILED.
type RollbackEntry struct {
	ID            string
	Environment   string
	Status        string
	FailureReason string
	FromReleaseID string
	FromBuildID   string
	ToReleaseID   string
	ToBuildID     string
	CreatedBy     string
	CreatedAt     time.Time
	FinishedAt    *time.Time
}

// RollbackHistory reads the recorded rollbacks of a project, newest first.
type RollbackHistory interface {
	ListRollbacks(ctx context.Context, tenantID, projectID string, limit int) ([]RollbackEntry, error)
}

const (
	rollbackListDefaultLimit = 50
	rollbackListMaxLimit     = 200
	// rollbackInterruptedAfter is how long a record may stay in progress. A
	// rollback is settled within 15 minutes of its start (the completion
	// timeout of the orchestrator); past that with room to spare, the process
	// that would have closed the record is gone.
	rollbackInterruptedAfter = 20 * time.Minute

	rollbackInterruptedReason = "The API stopped before this rollback was confirmed. The project status shows the release that is active."
)

// The statuses of a rollback in the API. The first three are stored; the
// last is derived from a record that was never closed.
const (
	rollbackStatusSucceeded   = "succeeded"
	rollbackStatusFailed      = "failed"
	rollbackStatusInterrupted = "interrupted"
)

type rollbackSide struct {
	ReleaseID string `json:"release_id"`
	BuildID   string `json:"build_id"`
}

type rollbackListItem struct {
	ID            string       `json:"id"`
	Environment   string       `json:"environment"`
	Status        string       `json:"status"`
	FailureReason string       `json:"failure_reason,omitempty"`
	From          rollbackSide `json:"from"`
	To            rollbackSide `json:"to"`
	CreatedBy     string       `json:"created_by"`
	CreatedAt     string       `json:"created_at"`
	FinishedAt    *string      `json:"finished_at"`
}

type rollbackListResponse struct {
	Rollbacks []rollbackListItem `json:"rollbacks"`
}

// RollbackHistoryHandler serves the list of a project's rollbacks.
type RollbackHistoryHandler struct {
	history RollbackHistory
	now     func() time.Time
}

// NewRollbackHistoryHandler creates the handler over the recorded rollbacks.
func NewRollbackHistoryHandler(history RollbackHistory) *RollbackHistoryHandler {
	return &RollbackHistoryHandler{history: history, now: time.Now}
}

// List handles GET /api/v1/projects/{name}/rollbacks.
func (h *RollbackHistoryHandler) List(w http.ResponseWriter, r *http.Request) {
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
	limit := rollbackListDefaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("limit must be a positive integer"))
			return
		}
		limit = min(parsed, rollbackListMaxLimit)
	}

	// The project id of builds, releases and rollbacks is the project name.
	entries, err := h.history.ListRollbacks(r.Context(), uc.TenantID, name, limit)
	if err != nil {
		slog.Error("rollback history: list failed", "error", err, "tenant_id", uc.TenantID, "project", name)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	now := h.now()
	resp := rollbackListResponse{Rollbacks: make([]rollbackListItem, 0, len(entries))}
	for _, e := range entries {
		resp.Rollbacks = append(resp.Rollbacks, rollbackItemOf(e, now))
	}
	httpio.WriteJSON(w, http.StatusOK, resp)
}

func rollbackItemOf(e RollbackEntry, now time.Time) rollbackListItem {
	item := rollbackListItem{
		ID:            e.ID,
		Environment:   e.Environment,
		FailureReason: e.FailureReason,
		From:          rollbackSide{ReleaseID: e.FromReleaseID, BuildID: e.FromBuildID},
		To:            rollbackSide{ReleaseID: e.ToReleaseID, BuildID: e.ToBuildID},
		CreatedBy:     e.CreatedBy,
		CreatedAt:     e.CreatedAt.UTC().Format(time.RFC3339),
	}
	if e.FinishedAt != nil {
		finished := e.FinishedAt.UTC().Format(time.RFC3339)
		item.FinishedAt = &finished
	}
	switch e.Status {
	case "SUCCEEDED":
		item.Status = rollbackStatusSucceeded
	case "FAILED":
		item.Status = rollbackStatusFailed
	default:
		item.Status = rollbackStatusInProgress
		if now.Sub(e.CreatedAt) > rollbackInterruptedAfter {
			item.Status = rollbackStatusInterrupted
			item.FailureReason = rollbackInterruptedReason
		}
	}
	return item
}
