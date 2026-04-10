// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package workqueue

import (
	"encoding/json"
	"testing"
)

func TestQueueName_Constants(t *testing.T) {
	// Verify queue names are stable strings (used in DB queries)
	queues := []QueueName{
		QueueBuildReconcile,
		QueueReleaseConverge,
		QueueEventPrune,
		QueueIdempotencyClean,
		QueueBuildReap,
		QueueReleaseReap,
	}

	seen := make(map[QueueName]bool)
	for _, q := range queues {
		if q == "" {
			t.Errorf("empty queue name")
		}
		if seen[q] {
			t.Errorf("duplicate queue name: %s", q)
		}
		seen[q] = true
	}
}

func TestWorkItem_PayloadUnmarshal(t *testing.T) {
	type BuildPayload struct {
		BuildID  string `json:"build_id"`
		TenantID string `json:"tenant_id"`
	}

	item := WorkItem{
		ID:       1,
		Queue:    QueueBuildReconcile,
		Payload:  json.RawMessage(`{"build_id":"bld_123","tenant_id":"tn_abc"}`),
		Attempts: 1,
	}

	var payload BuildPayload
	if err := json.Unmarshal(item.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if payload.BuildID != "bld_123" {
		t.Errorf("expected build_id bld_123, got %s", payload.BuildID)
	}
	if payload.TenantID != "tn_abc" {
		t.Errorf("expected tenant_id tn_abc, got %s", payload.TenantID)
	}
}
