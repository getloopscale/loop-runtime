// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package conditions

// Standard condition types for the kstatus universal summary.
// Every LoopRuntime CRD that adopts the summary pattern exposes these three
// in its status.conditions, computed by SetReadinessSummary from the
// per-step detail conditions.
//
// Semantics mirror the kpt CRD Status Convention
// (https://kpt.dev/reference/schema/crd-status-convention/):
//
//   - Ready=True       → fully reconciled       ("Current")
//   - Reconciling=True → controller is working  ("InProgress")
//   - Stalled=True     → manual intervention    ("Failed")
const (
	// TypeReady — kstatus-positive: True means fully reconciled.
	TypeReady = "Ready"
	// TypeReconciling — kstatus-positive-progress: True means in progress.
	TypeReconciling = "Reconciling"
	// TypeStalled — kstatus-negative: True means manual intervention.
	TypeStalled = "Stalled"
)

// Standard reason tokens. SetReadinessSummary classifies detail
// conditions by Reason — failed/stalled/degraded reasons cascade to
// summary Stalled=True; in-progress reasons cascade to Reconciling=True.
const (
	// ReasonReady — used when a detail or summary is fully True.
	ReasonReady = "Ready"
	// ReasonReconciling — generic in-progress reason.
	ReasonReconciling = "Reconciling"
	// ReasonProgressing — alias for Reconciling using kpt vocabulary.
	ReasonProgressing = "Progressing"
	// ReasonWaiting — controller is waiting for an external event
	// (e.g., DNS propagation, certificate issuance).
	ReasonWaiting = "Waiting"
	// ReasonNotFound — a referenced resource has not been observed yet.
	// Treated as in-progress (controller may be racing creation).
	ReasonNotFound = "NotFound"
	// ReasonUnreachable — a referenced resource exists but is not
	// reachable (network, auth). Treated as in-progress with bounded
	// retries; eventually cascades to Stalled if persistent.
	ReasonUnreachable = "Unreachable"
	// ReasonFailed — terminal failure for the detail. Cascades to
	// summary Stalled=True.
	ReasonFailed = "Failed"
	// ReasonStalled — explicit stalled reason. Cascades to Stalled=True.
	ReasonStalled = "Stalled"
	// ReasonDegraded — partial-failure reason. The resource is
	// observable but not fully healthy. Cascades to Stalled=True per
	// the application controller's computePhase mapping.
	ReasonDegraded = "Degraded"
)

// stalledReasons is the closed set of detail reasons that cascade to
// summary Stalled=True. Other false-status reasons (Reconciling,
// Progressing, Waiting, NotFound, Unreachable, etc.) cascade to
// summary Reconciling=True instead.
var stalledReasons = map[string]struct{}{
	ReasonFailed:   {},
	ReasonStalled:  {},
	ReasonDegraded: {},
}
