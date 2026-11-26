// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/api/internal/scrub"
	"github.com/getloopscale/loopzero/api/internal/timeline/aggregator"
)

// timelineResumeStore is what the resumer needs from the event journal: the
// builds whose timeline is open, their events, and the two write paths of the
// aggregator's sink.
type timelineResumeStore interface {
	timelineEventReader
	ListBuildsWithoutLiveTimeline(ctx context.Context, since time.Time) ([]string, error)
	Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	AppendOnce(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, bool, error)
}

// timelineResumer finishes the deploy timelines a restart left open.
//
// The aggregator that writes a build's timeline is a goroutine of the process
// that took the submit. When that process stops mid-deploy, the orchestrator's
// own events still arrive (another loop writes them), and the timeline events
// they imply are never written. The resumer runs on the leader, looks for
// streams in that state, and starts an aggregator for each. That aggregator
// walks the whole stream again and the sink writes only the events that are
// missing, so it is safe next to an aggregator that is still alive.
type timelineResumer struct {
	store  timelineResumeStore
	logger *slog.Logger

	// interval is the time between two passes.
	interval time.Duration
	// grace is how old an event must be before the state it implies counts as
	// missing. A live aggregator writes it within a few poll intervals.
	grace time.Duration
	// window is how far back a build may have started.
	window time.Duration
	// maxRun bounds one resumed aggregator, for a deploy that never ends.
	maxRun time.Duration
	// pollInterval is the resumed aggregator's poll interval.
	pollInterval time.Duration

	// attempted holds the builds already taken. A timeline the state machine
	// refuses to finish stays open, and taking it on every pass would write a
	// degraded event each time.
	attempted map[string]bool
	// run starts the aggregator of one build. Tests replace it.
	run func(ctx context.Context, buildID string)
}

func newTimelineResumer(store timelineResumeStore, logger *slog.Logger) *timelineResumer {
	if logger == nil {
		logger = slog.Default()
	}
	r := &timelineResumer{
		store:        store,
		logger:       logger.With("component", "deploy_timeline_resumer"),
		interval:     time.Minute,
		grace:        30 * time.Second,
		window:       24 * time.Hour,
		maxRun:       2 * time.Hour,
		pollInterval: 2 * time.Second,
		attempted:    make(map[string]bool),
	}
	r.run = r.resume
	return r
}

// Start runs a pass now and one every interval until ctx ends.
func (r *timelineResumer) Start(ctx context.Context) {
	r.tick(ctx)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

// tick takes every build whose stream implies timeline states nobody wrote.
func (r *timelineResumer) tick(ctx context.Context) {
	now := time.Now().UTC()
	builds, err := r.store.ListBuildsWithoutLiveTimeline(ctx, now.Add(-r.window))
	if err != nil {
		r.logger.Error("listing builds with an open timeline failed", "err", err)
		return
	}
	for _, buildID := range builds {
		if r.attempted[buildID] {
			continue
		}
		events, err := r.store.GetAfter(ctx, buildID, 0)
		if err != nil {
			r.logger.Error("reading the events of a build failed", "build_id", buildID, "err", err)
			continue
		}
		missing := unwrittenTimelineStates(events, now.Add(-r.grace))
		if len(missing) == 0 {
			continue
		}
		r.attempted[buildID] = true
		r.logger.Info("resuming a deploy timeline", "build_id", buildID, "missing_states", missing)
		go r.run(ctx, buildID)
	}
}

// resume walks one build's stream with a new aggregator until its timeline
// ends, ctx ends, or maxRun passes.
func (r *timelineResumer) resume(ctx context.Context, buildID string) {
	ctx, cancel := context.WithTimeout(ctx, r.maxRun)
	defer cancel()
	reader := aggregator.NewReleaseDBReader(&sseTimelineQuerier{eventStore: r.store})
	reader.PollInterval = r.pollInterval
	aggregator.New(buildID, aggregator.NewSSEEventSink(r.store), scrub.ScrubEventWithCount,
		[]aggregator.SourceReader{reader}).Run(ctx)
}

// unwrittenTimelineStates returns, in order, the timeline states the stream's
// orchestrator events imply and no timeline event records. Only events created
// before cutoff count: the aggregator of a deploy in flight writes a state a
// few seconds after the event behind it.
func unwrittenTimelineStates(events []ports.SSEEvent, cutoff time.Time) []string {
	written := make(map[string]bool)
	for _, ev := range events {
		if strings.HasPrefix(ev.EventType, "deploy_timeline.") {
			written[ev.EventType] = true
		}
	}
	var missing []string
	seen := make(map[string]bool)
	for _, ev := range events {
		if !ev.CreatedAt.Before(cutoff) {
			continue
		}
		for _, state := range eventTypeToCanonicalTransitions(ev.EventType) {
			if seen[state] {
				continue
			}
			seen[state] = true
			if !written[aggregator.TimelineEventType(state)] {
				missing = append(missing, state)
			}
		}
	}
	return missing
}
