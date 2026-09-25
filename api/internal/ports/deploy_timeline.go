// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package ports

import (
	"encoding/json"
	"errors"
	"time"
)

// DeployTimelineEvent is the canonical SSE-event row of a deploy timeline.
// ID is monotonic per (tenant, deploy). EventType follows the convention
// "deploy_timeline.<phase>.<status>". It lives here, below both the timeline
// route and the aggregator store, so neither has to import the other.
type DeployTimelineEvent struct {
	ID        int64           `json:"id"`
	EventType string          `json:"event_type"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"created_at"`
}

// ErrDeployNotFound is the sentinel for an unknown deploy_id OR a deploy
// owned by another tenant. The handler renders 404 in both cases so a
// non-owner cannot learn that a deploy exists.
var ErrDeployNotFound = errors.New("deploy timeline: not found")
