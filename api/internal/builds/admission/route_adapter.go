// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package admission

import (
	"context"
	"time"

	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// RouteAdapter wraps a BuildGate to satisfy the minimal contract the
// routes package defines for its admission checker. It translates a
// gate Result into the (admitted, retryAfter, code, message) tuple the
// HTTP handler wants so routes does not have to import this package.
//
// Usage in main.go:
//
//	gate := admission.NewBuildGate(querier, admission.DefaultConfig(), cfg.AdmissionControl)
//	buildHandler.WithAdmission(admission.NewRouteAdapter(gate), cellID)
type RouteAdapter struct {
	gate *BuildGate
}

// NewRouteAdapter wraps the given gate. Passing nil produces nil so
// callers can safely chain `WithAdmission(NewRouteAdapter(gate), cellID)`
// even when admission control is disabled.
func NewRouteAdapter(gate *BuildGate) *RouteAdapter {
	if gate == nil {
		return nil
	}
	return &RouteAdapter{gate: gate}
}

// Check implements the contract the routes package expects. It maps
// gate decisions to typed error codes so the handler can build a
// consistent HTTP 429 response:
//
//   - RejectCellAtCapacity  → loopzeroerrors.ErrCellAtCapacity
//   - RejectTenantQuotaExceeded → loopzeroerrors.ErrTenantBuildQuota
//
// The reason string from the gate is included in the message so
// operators and end users get a concrete "X / Y builds in flight"
// explanation. Rule 3 (no internal terms) is still respected because
// the error Message templates in errors.go are the user-facing strings;
// the gate reason is only appended for debug context and never
// references internal identifiers.
func (a *RouteAdapter) Check(ctx context.Context, cellID, tenantID string) (
	admitted bool, retryAfter time.Duration, code, message string, err error,
) {
	if a == nil || a.gate == nil {
		return true, 0, "", "", nil
	}
	res, gateErr := a.gate.Check(ctx, cellID, tenantID)
	if gateErr != nil {
		// Propagate so the handler can log and fail-open.
		return true, 0, "", "", gateErr
	}
	switch res.Decision {
	case Admit:
		return true, 0, "", "", nil
	case RejectCellAtCapacity:
		return false, res.RetryAfter,
			loopzeroerrors.ErrCellAtCapacity.Code,
			loopzeroerrors.ErrCellAtCapacity.Message + " (" + res.Reason + ")",
			nil
	case RejectTenantQuotaExceeded:
		return false, res.RetryAfter,
			loopzeroerrors.ErrTenantBuildQuota.Code,
			loopzeroerrors.ErrTenantBuildQuota.Message + " (" + res.Reason + ")",
			nil
	default:
		// Unknown decision — fail-open with a clear log hook. The
		// caller's logger will record the full res.Reason.
		return true, 0, "", "", nil
	}
}
