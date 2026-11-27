// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package leak

import "net/http"

// UpstreamState separates "the engine is down" from "the dashboard is down".
//
// Measured 2026-08-10: syncing the Caddyfile over the server left loop-console
// with no reachable upstream, and `/engine/*` went down with it, because the
// engine path crosses the dashboard. Both showed as 502 and the probe could not
// say which one had failed. The two call for different actions — engine down is
// a deploy reconciliation in the core cluster, dashboard down is the edge proxy
// — and a report that confuses them sends the operator to the wrong place
// exactly when time matters.
//
// The separation is possible because the dashboard's PRODUCT routes do not
// cross the engine: loop-console proxies `/engine/*` to loop-runtime-api while `/v1/*`
// uses its own handlers. A product route answering while `/engine/*` does not is
// evidence the dashboard is up.
//
// The limit: this classifies TWO status codes, not the cause. A 502 may be an
// upstream that is down, slow or misrouted; the probe does not tell those
// apart. It tells WHICH of the two surfaces failed.
type UpstreamState int

const (
	// UpstreamBothUp — neither surface has a gateway failure.
	UpstreamBothUp UpstreamState = iota
	// UpstreamEngineDown — the dashboard answers and the engine path does not.
	UpstreamEngineDown
	// UpstreamDashboardDown — both fail; since the engine crosses the
	// dashboard, the common cause is the dashboard.
	UpstreamDashboardDown
	// UpstreamInconclusive — the reading contradicts the known topology.
	UpstreamInconclusive
)

// gatewayFailure reports whether the edge could not reach what sits behind it.
// 502/503/504 are what the proxy emits then; a 500 comes from the service
// itself, which is therefore up.
func gatewayFailure(status int) bool {
	switch status {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// ClassifyUpstream separates the two failures from one probe on the engine
// path and one on a dashboard product route.
func ClassifyUpstream(engineStatus, dashboardStatus int) UpstreamState {
	engineBad := gatewayFailure(engineStatus)
	dashBad := gatewayFailure(dashboardStatus)

	switch {
	case !engineBad && !dashBad:
		return UpstreamBothUp
	case engineBad && dashBad:
		return UpstreamDashboardDown
	case engineBad && !dashBad:
		return UpstreamEngineDown
	default:
		// The dashboard failing while the engine answers contradicts the
		// topology (`/engine/*` crosses the dashboard). Inventing a verdict
		// here would claim what the probe does not know.
		return UpstreamInconclusive
	}
}

// Explanation tells the operator what the reading means and where to look.
func (s UpstreamState) Explanation() string {
	switch s {
	case UpstreamBothUp:
		return "engine and dashboard reachable through the edge"
	case UpstreamEngineDown:
		return "the dashboard answers and the engine path does not — look at loop-runtime-api in the core cluster, not the edge proxy"
	case UpstreamDashboardDown:
		return "both surfaces fail; since /engine/* crosses the dashboard, start with loop-console and the edge proxy"
	case UpstreamInconclusive:
		return "the reading contradicts the known topology (/engine/* crosses the dashboard) — probe again before acting"
	}
	return "unknown state"
}
