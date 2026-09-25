// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"context"
	"net/http"
	"time"

	"github.com/getloopscale/loopzero/api/internal/httpio"

	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// BuildDayCount is the route-level per-day build (deploy) count. Route-level
// type (not the db BuildStatsByDayRow) to avoid the db→routes import cycle.
type BuildDayCount struct {
	Day   time.Time // UTC calendar day (midnight)
	Count int
}

// BuildStatsStore abstracts the tenant-scoped deploys aggregate (DIP). The
// concrete implementation runs the sqlc BuildStatsByDay query, whose
// `WHERE tenant_id = $1` predicate is the ONLY isolation barrier under the
// BYPASSRLS engine pool (#106/#111). `since` is the window lower bound.
type BuildStatsStore interface {
	BuildStatsByDay(ctx context.Context, tenantID string, since time.Time) ([]BuildDayCount, error)
}

// BuildStatsHandler serves GET /api/v1/builds/stats.
type BuildStatsHandler struct {
	stats BuildStatsStore
}

// NewBuildStatsHandler constructs the handler.
func NewBuildStatsHandler(stats BuildStatsStore) *BuildStatsHandler {
	return &BuildStatsHandler{stats: stats}
}

// dayBucket is one entry of the dense, zero-filled by_day series.
type dayBucket struct {
	Date  string `json:"date"` // YYYY-MM-DD, UTC
	Count int    `json:"count"`
}

// buildStatsResponse is the JSON shape for GET /api/v1/builds/stats
// (MEET-2026-07-03-003 D2). by_day is dense (exactly window_days entries),
// zero-filled, ascending; last_7d_count is derived from the same series so it
// can never disagree with the sparkline.
type buildStatsResponse struct {
	WindowDays  int         `json:"window_days"`
	Timezone    string      `json:"timezone"`
	ByDay       []dayBucket `json:"by_day"`
	Last7dCount int         `json:"last_7d_count"`
	TotalCount  int         `json:"total_count"`
}

// windowDaysFor maps the enumerated ?window value to a day count. The enum is
// closed (7d/30d/90d) so an arbitrary/unbounded range can never table-scan;
// empty defaults to 30d. Returns ok=false for any non-enumerated value.
func windowDaysFor(window string) (int, bool) {
	switch window {
	case "", "30d":
		return 30, true
	case "7d":
		return 7, true
	case "90d":
		return 90, true
	default:
		return 0, false
	}
}

// Stats handles GET /api/v1/builds/stats?window={7d,30d,90d}.
func (h *BuildStatsHandler) Stats(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Fail-closed: a missing tenant context MUST NOT run an unscoped aggregate
	// (the #106/#111 class — RLS is not a backstop). 401, never an empty/global scan.
	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	windowDays, ok := windowDaysFor(r.URL.Query().Get("window"))
	if !ok {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInvalidRequest.Wrap("window must be one of: 7d, 30d, 90d"))
		return
	}

	// UTC calendar-day window: [firstDay .. today], inclusive → exactly windowDays days.
	today := time.Now().UTC().Truncate(24 * time.Hour)
	firstDay := today.AddDate(0, 0, -(windowDays - 1))

	rows, err := h.stats.BuildStatsByDay(ctx, uc.TenantID, firstDay)
	if err != nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	// Index the sparse DB rows by UTC day so we can zero-fill.
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[row.Day.UTC().Format("2006-01-02")] += row.Count
	}

	byDay := make([]dayBucket, 0, windowDays)
	total := 0
	last7 := 0
	for i := 0; i < windowDays; i++ {
		day := firstDay.AddDate(0, 0, i)
		key := day.Format("2006-01-02")
		c := counts[key]
		byDay = append(byDay, dayBucket{Date: key, Count: c})
		total += c
		// last 7 calendar days = the final min(7, windowDays) buckets.
		if windowDays-i <= 7 {
			last7 += c
		}
	}

	httpio.WriteJSON(w, http.StatusOK, buildStatsResponse{
		WindowDays:  windowDays,
		Timezone:    "UTC",
		ByDay:       byDay,
		Last7dCount: last7,
		TotalCount:  total,
	})
}
