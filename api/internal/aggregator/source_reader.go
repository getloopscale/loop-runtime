// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package aggregator implements the per-deploy aggregator goroutine that
// fan-ins phase-status events from N source-reader adapters (DIP boundary)
// and emits canonical SSE PhaseEvents to the EventStore.
//
// Plan ADR D1: single linear top-level FSM. ADR D2: per-deploy aggregator
// goroutine. ADR D3: regex-based scrub denylist. ADR D4a: scrub at WRITE
// time before EventStore.Append. ADR D7: 5 source-reader adapters via
// DIP (Strategy pattern).
package aggregator

import (
	"context"
	"time"
)

// SourceID identifies one of the 5 canonical source adapters. The set is
// closed (no operator extension); adding a new source requires a plan
// amendment per Phase-D ADR D7.
type SourceID string

const (
	// SourceLokiMarkers reads ::loopzero:: marker log lines via the Loki
	// HTTP API (workload+build clusters).
	SourceLokiMarkers SourceID = "loki_markers"
	// SourceReleaseDB tails the releases table for status transitions
	// (READY → PENDING → ACTIVE / ROLLING_BACK / SUPERSEDED).
	SourceReleaseDB SourceID = "release_db"
	// SourceArgoCDApp polls ArgoCD Application sync_status + health_status.
	SourceArgoCDApp SourceID = "argocd_app"
	// SourceLoopZeroAppInformer watches LoopZeroApplication CRD via informer.
	SourceLoopZeroAppInformer SourceID = "loopzeroapp_informer"
	// SourceStatusURLProbe HTTP-GETs the live URL with budget per ADR D6.
	SourceStatusURLProbe SourceID = "status_url_probe"
)

// PhaseEvent is the canonical event shape every adapter emits. Fields are
// PRE-SCRUB — the aggregator runs its ScrubFunc (scrub.ScrubEventWithCount) before persistence
// per ADR D4 (write-time scrub). NEVER persist a PhaseEvent without
// calling scrub first.
//
// Phase + Status are stringly-typed deliberately so adapters can emit
// raw source-format values; the FSM Transition() validates them.
type PhaseEvent struct {
	// Phase canonical name (must map to statemachine.DeployTimelinePhase
	// after scrub). Empty string = "no phase signal" — aggregator skips.
	Phase string
	// Status canonical (must map to DeployTimelinePhaseStatus). Empty =
	// heartbeat-only event (keeps adapter alive for stale-source EC-2).
	Status string
	// DegradedReason carries source-specific failure text (pre-scrub).
	DegradedReason string
	// SubstateMessage carries source-specific UX text (pre-scrub).
	SubstateMessage string
	// Timestamp is the event's source-system time. Adapters MUST set
	// this — aggregator will reject zero-time events (heartbeat is
	// distinguished by empty Status, not zero Timestamp).
	Timestamp time.Time
	// Source identifies the adapter (auto-set by aggregator from
	// SourceReader.ID() — adapters need NOT populate).
	Source SourceID
}

// SourceReader is the DIP contract every source adapter implements.
//
// Stream is invoked once per deploy by the aggregator goroutine in its
// own child goroutine. The adapter MUST:
//
//  1. Push PhaseEvent values onto `out` until the source signals
//     terminal state (live/completed OR any-phase/failed) OR ctx.Done()
//     fires. Closing `out` is the AGGREGATOR's job, never the adapter's.
//  2. Return nil on graceful completion (terminal-state reached).
//  3. Return ctx.Err() on cancellation.
//  4. Return a non-nil error for any other failure; the aggregator
//     emits a DEGRADED phase event and exits per ADR D1 § Drawbacks.
//
// Adapters MUST be safe to call concurrently across distinct deploys.
// Within a single deploy, Stream is called exactly once.
type SourceReader interface {
	ID() SourceID
	Stream(ctx context.Context, deployID string, out chan<- PhaseEvent) error
}
