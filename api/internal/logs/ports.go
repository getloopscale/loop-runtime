// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package logs is the build-log feature: reading a build's log lines, paged or
// streamed. Its handlers live in logshttp and its Loki client in logs/loki; the
// ports here are what the handlers need from a log backend.
//
// History: these types left routes/logs.go for the domain package (T1.3 of
// deep-review-remediation-plan, find_arch_001 + find_dep_routes_type_hub), and
// came here when api/internal was organized by feature (2026-09-25), since only
// this feature consumes them.
package logs

import "context"

// LogEntry represents a single log line from a build.
type LogEntry struct {
	Step      string `json:"step"`
	Stream    string `json:"stream"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
}

// LogQueryOpts controls log retrieval.
type LogQueryOpts struct {
	Limit int
	Step  string
	// SinceSeconds restricts the query to entries newer than now()-SinceSeconds.
	// Zero or negative means "no time-window restriction" — the Loki client
	// applies its own default (last 1h). Beta Logs Gate Criterion 7: gives
	// the operator a "--since 24h|7d|30d" knob on `loopzero logs`.
	SinceSeconds int64
	// Debug toggles the LogQL filter that hides non-user-facing log
	// lines. Plan J-17 Layer 3 (2026-06-06): user view (Debug=false)
	// shows only lines emitted with the `::loopzero::` marker via the
	// pipeline's wrapper (see api/internal/builds/argo/loopzero_log.go) — those
	// are tagged in Loki with the `loopzero_phase` label. Debug=true
	// bypasses the filter and returns every container line including
	// main-container raw stdout. Reserved for operator troubleshooting;
	// route handler must gate this with admin auth.
	Debug bool
}

// LogQuerier abstracts log retrieval from Loki (DIP).
type LogQuerier interface {
	QueryBuildLogs(ctx context.Context, buildID string, opts LogQueryOpts) ([]LogEntry, error)
}

// StreamLogQuerier abstracts incremental log retrieval from Loki (DIP).
// Unlike LogQuerier which fetches a batch, this supports streaming by
// querying logs since a given nanosecond timestamp.
//
// NOT embedded with LogQuerier — fake implementations in tests only need
// QueryBuildLogsSince. The Loki concrete implementation satisfies BOTH
// interfaces independently.
type StreamLogQuerier interface {
	QueryBuildLogsSince(ctx context.Context, queryKey string, sinceNano int64, limit int) ([]LogEntry, error)
}
