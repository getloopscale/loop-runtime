// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"strings"
	"testing"

	"github.com/loopzero/loopzero/pkg/statemachine"
)

func TestReaperReason_QueueTimeout(t *testing.T) {
	reason := reaperReason(statemachine.BuildQueueTimeout)
	if !strings.Contains(reason, "queue") {
		t.Errorf("expected queue-related reason, got %q", reason)
	}
}

func TestReaperReason_BuildTimeout(t *testing.T) {
	reason := reaperReason(statemachine.BuildTimeout)
	if !strings.Contains(reason, "running") {
		t.Errorf("expected running-related reason, got %q", reason)
	}
}

func TestReaperReason_CancelTimeout(t *testing.T) {
	reason := reaperReason(statemachine.BuildCancelTimeout)
	if !strings.Contains(reason, "cancellation") {
		t.Errorf("expected cancellation-related reason, got %q", reason)
	}
}

func TestReaperReason_UnknownEvent(t *testing.T) {
	reason := reaperReason(statemachine.Event("some_unknown_event"))
	if !strings.Contains(reason, "timeout") {
		t.Errorf("expected generic timeout reason, got %q", reason)
	}
}
