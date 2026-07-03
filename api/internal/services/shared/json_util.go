// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package shared

import (
	"encoding/json"
	"log/slog"
)

// SafeJSONMarshal marshals v to JSON. If marshalling fails, it logs the error
// and returns a fallback empty JSON object so SSE events are still emitted.
// M9 Step 6: extracted from release_activator.go into the shared leaf because
// both the build and release contexts marshal SSE event payloads through it.
func SafeJSONMarshal(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("json.Marshal failed for SSE event payload", "error", err)
		return json.RawMessage("{}")
	}
	return data
}
