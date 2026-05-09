// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Logs domain types — moved from routes/logs.go per T1.3 of
// deep-review-remediation-plan (closes find_arch_001 + find_dep_routes_type_hub).
//
// Pre-T1.3: routes/ acted as a type-hub, importing-and-being-imported by
// 9 files in 5 lower layers (DIP inversion). Moving these types here
// breaks the cycle: services/db/k8s/loki/github now import domain/
// directly; routes/ re-exports as type aliases for back-compat.

package domain

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
