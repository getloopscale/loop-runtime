// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package aggregator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/metrics"
	"github.com/getloopscale/loop-runtime/api/internal/scrub"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// EventSink is the minimal subset of EventStore the aggregator needs.
// Decoupled here so tests can use a memory implementation without
// importing the SSE package (which imports db).
//
// Plan ADR D2 (per-deploy aggregator owns one EventStore reference) +
// ADR D8 (EventStore enforces tenant_id at SQL level — aggregator
// passes deploy_id; tenant filtering is the EventStore's invariant).
type EventSink interface {
	AppendDeployTimelineEvent(ctx context.Context, deployID, eventType string, data json.RawMessage) (int64, error)
}

// AggregatorDegradedEventType is the event an aggregator writes when it is in
// trouble itself: a panic, a source that failed, a transition the state machine
// refused. It reports an occurrence, so one stream can hold several.
const AggregatorDegradedEventType = "deploy_timeline.aggregator.degraded"

// ScrubFunc removes internal terms from an event and reports how many
// replacements it made (fed to loop_runtime_deploy_timeline_scrub_replacements_total).
// Production passes scrub.ScrubEventWithCount; tests may inject another.
type ScrubFunc func(scrub.PhaseEvent) (scrub.PhaseEvent, int)

// Aggregator is the per-deploy goroutine that fan-ins phase events from
// N SourceReaders, applies scrub at WRITE time (ADR D4a), drives a
// strict FSM (ADR D1), and persists canonical SSE events via EventSink.
//
// NOT thread-safe across instances — one Aggregator per deploy, owned
// by the spawning goroutine. Within Run() the aggregator multiplexes N
// adapter goroutines via a fan-in channel.
//
// DEFERRED — ADR D5 EC-2 per-source heartbeat watchdog (30s OR 6×poll
// silence → emit DEGRADED with substate "source X silent"). Empty-Phase
// heartbeat events already flow through (see handleEvent below); the
// watchdog goroutine that PROMOTES silence-without-heartbeat into a
// DEGRADED emission lives in a follow-up commit per cross-validator
// review feedback. Tracked via the plan's Open Questions §
// stale-source-watchdog.
type Aggregator struct {
	deployID  string
	sink      EventSink
	scrubFunc ScrubFunc
	readers   []SourceReader
	state     *statemachine.DeployTimelineState
	logger    *slog.Logger
}

// New constructs an Aggregator. ScrubFunc and readers must be non-nil.
func New(deployID string, sink EventSink, scrubFunc ScrubFunc, readers []SourceReader) *Aggregator {
	return &Aggregator{
		deployID:  deployID,
		sink:      sink,
		scrubFunc: scrubFunc,
		readers:   readers,
		state:     statemachine.NewDeployTimelineState(),
		logger:    slog.Default().With("component", "deploy_timeline_aggregator", "deploy_id", deployID),
	}
}

// Run drives the aggregator synchronously until either:
//   - the FSM reaches a terminal state (live/completed OR any/failed OR
//     non-Live/canceled),
//   - all adapter goroutines exit (channel close),
//   - ctx is canceled.
//
// Returns only after the goroutine has fully drained — callers may spawn
// it inside `go agg.Run(ctx)` for fire-and-forget per ADR D2.
func (a *Aggregator) Run(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			a.logger.Error("aggregator goroutine panic recovered",
				"panic", r, "stack", string(debug.Stack()))
			a.emitAggregatorDegraded(ctx, fmt.Sprintf("aggregator panic: %v", r))
		}
	}()

	out := make(chan readerEvent, 32)
	var wg sync.WaitGroup
	for _, r := range a.readers {
		wg.Add(1)
		go func(r SourceReader) {
			defer wg.Done()
			defer func() {
				if rec := recover(); rec != nil {
					a.logger.Error("adapter panic recovered",
						"source", r.ID(), "panic", rec, "stack", string(debug.Stack()))
					select {
					case out <- readerEvent{source: r.ID(), panicReason: fmt.Sprintf("%v", rec)}:
					case <-ctx.Done():
					}
				}
			}()
			adapter := make(chan PhaseEvent, 8)
			done := make(chan error, 1)
			go func() {
				defer func() {
					if rec := recover(); rec != nil {
						a.logger.Error("adapter Stream panic recovered (inner)",
							"source", r.ID(), "panic", rec, "stack", string(debug.Stack()))
						done <- fmt.Errorf("adapter panic: %v", rec)
					}
					close(adapter)
				}()
				done <- r.Stream(ctx, a.deployID, adapter)
			}()
			for e := range adapter {
				e.Source = r.ID()
				select {
				case out <- readerEvent{source: r.ID(), event: e}:
				case <-ctx.Done():
					return
				}
			}
			if err := <-done; err != nil && ctx.Err() == nil {
				a.logger.Warn("adapter Stream returned error",
					"source", r.ID(), "err", err)
				select {
				case out <- readerEvent{source: r.ID(), streamErr: err.Error()}:
				case <-ctx.Done():
				}
			}
		}(r)
	}

	// Closer goroutine — when all adapters exit, close `out` so the
	// fan-in loop below terminates.
	closeDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(out)
		close(closeDone)
	}()

	for {
		select {
		case ev, ok := <-out:
			if !ok {
				return
			}
			a.handleEvent(ctx, ev)
			if a.state.IsTerminal() {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

type readerEvent struct {
	source      SourceID
	event       PhaseEvent
	streamErr   string
	panicReason string
}

// handleEvent applies scrub + FSM Transition + emits canonical SSE.
// Invalid FSM transitions panic; we recover here and emit DEGRADED.
func (a *Aggregator) handleEvent(ctx context.Context, ev readerEvent) {
	if ev.panicReason != "" {
		a.emitAggregatorDegraded(ctx, fmt.Sprintf("adapter %s panic: %s", ev.source, ev.panicReason))
		return
	}
	if ev.streamErr != "" {
		a.emitAggregatorDegraded(ctx, fmt.Sprintf("adapter %s error: %s", ev.source, ev.streamErr))
		return
	}
	if ev.event.Phase == "" {
		// Heartbeat-only — used by adapters to keep stale-source watchdog quiet.
		return
	}
	scrubbedIn := scrub.PhaseEvent{
		Phase:           ev.event.Phase,
		Status:          ev.event.Status,
		DegradedReason:  ev.event.DegradedReason,
		SubstateMessage: ev.event.SubstateMessage,
	}
	scrubbed, replaced := a.scrubFunc(scrubbedIn)
	if replaced > 0 {
		metrics.DeployTimelineScrubReplacementsTotal.WithLabelValues(scrubbed.Phase).Add(float64(replaced))
	}

	phase := statemachine.DeployTimelinePhase(scrubbed.Phase)
	status := statemachine.DeployTimelinePhaseStatus(scrubbed.Status)

	func() {
		defer func() {
			if r := recover(); r != nil {
				a.logger.Warn("FSM invalid transition — emitting DEGRADED",
					"current_phase", a.state.Phase(),
					"current_status", a.state.Status(),
					"target_phase", phase,
					"target_status", status,
					"panic", r)
				a.emitAggregatorDegraded(ctx,
					fmt.Sprintf("invalid transition %s/%s -> %s/%s",
						a.state.Phase(), a.state.Status(), phase, status))
			}
		}()
		if err := a.state.Transition(phase, status); err != nil {
			a.logger.Warn("FSM transition error",
				"phase", phase, "status", status, "err", err)
			a.emitAggregatorDegraded(ctx, err.Error())
			return
		}
		a.emitPhaseEvent(ctx, scrubbed, ev.source, ev.event.Timestamp)
	}()
}

// emitPhaseEvent persists a canonical SSE event. EventType convention:
// "deploy_timeline.<phase>.<status>". Payload includes deploy_id, source,
// timestamp, scrubbed reason/message.
func (a *Aggregator) emitPhaseEvent(ctx context.Context, e scrub.PhaseEvent, source SourceID, ts time.Time) {
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	payload := map[string]any{
		"deploy_id":        a.deployID,
		"phase":            e.Phase,
		"status":           e.Status,
		"degraded_reason":  e.DegradedReason,
		"substate_message": e.SubstateMessage,
		"source":           string(source),
		"timestamp":        ts.UTC().Format(time.RFC3339Nano),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		a.logger.Error("payload marshal failed", "err", err)
		return
	}
	eventType := fmt.Sprintf("deploy_timeline.%s.%s", e.Phase, e.Status)
	if _, err := a.sink.AppendDeployTimelineEvent(ctx, a.deployID, eventType, data); err != nil {
		a.logger.Error("AppendDeployTimelineEvent failed",
			"event_type", eventType, "err", err)
		return
	}
	metrics.DeployTimelineEventsTotal.WithLabelValues(e.Phase, e.Status, string(source)).Inc()
}

// emitAggregatorDegraded persists a synthetic event signaling internal
// trouble (panic, stream error, invalid transition). Frontends render
// this as "deploy timeline temporarily unavailable" per ADR D6.
func (a *Aggregator) emitAggregatorDegraded(ctx context.Context, reason string) {
	scrubbed, _ := a.scrubFunc(scrub.PhaseEvent{
		DegradedReason: reason,
	})
	payload := map[string]any{
		"deploy_id":       a.deployID,
		"degraded_reason": scrubbed.DegradedReason,
		"timestamp":       time.Now().UTC().Format(time.RFC3339Nano),
	}
	data, _ := json.Marshal(payload)
	if _, err := a.sink.AppendDeployTimelineEvent(ctx, a.deployID, AggregatorDegradedEventType, data); err != nil {
		a.logger.Error("emit DEGRADED failed", "err", err)
	}
	metrics.DeployTimelineAggregatorPanicTotal.WithLabelValues("aggregator").Inc()
}
