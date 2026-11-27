// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package events

import (
	"encoding/json"
	"log/slog"
)

// SafeJSONMarshal marshals v to JSON. If marshalling fails, it logs the error
// and returns a fallback empty JSON object so SSE events are still emitted.
//
// It lives here because it is an SSE-event-payload helper — the
// build and release contexts both marshal deployment SSE payloads through it,
// and this package is already the shared home of the SSE deployment event
// contracts (see deployment.go). Before M9 post-audit §7 Step 2 it lived in
// api/internal/services/shared, which conflated a pure serialization utility
// with the deploy DTOs and gave that leaf a fake reason to be depended on.
func SafeJSONMarshal(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("json.Marshal failed for SSE event payload", "error", err)
		return json.RawMessage("{}")
	}
	return data
}
