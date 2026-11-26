// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package sse

import "github.com/getloopscale/loop-runtime/api/internal/ports"

// SSEEvent and EventStore are the journal contract the streams read. They are
// declared in ports so the Postgres implementation can live in db/store with
// the rest of the SQL; these aliases keep the name the streaming code uses.
type (
	SSEEvent   = ports.SSEEvent
	EventStore = ports.EventStore
)
