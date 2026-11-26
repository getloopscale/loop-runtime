// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package aggregator

import (
	"context"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// TestSourceID_String validates the canonical 5-source enum values.
func TestSourceID_String(t *testing.T) {
	tests := []struct {
		id   SourceID
		want string
	}{
		{SourceLokiMarkers, "loki_markers"},
		{SourceReleaseDB, "release_db"},
		{SourceArgoCDApp, "argocd_app"},
		{SourceLoopZeroAppInformer, "loopzeroapp_informer"},
		{SourceStatusURLProbe, "status_url_probe"},
	}
	for _, tt := range tests {
		if got := string(tt.id); got != tt.want {
			t.Errorf("SourceID %v = %q; want %q", tt.id, got, tt.want)
		}
	}
}

// TestPhaseEvent_DefaultZero confirms zero-value safety — emitted by
// adapters MUST be safe to compare/scrub even when fields are unset.
func TestPhaseEvent_DefaultZero(t *testing.T) {
	var e PhaseEvent
	if e.Phase != "" || e.Status != "" || e.DegradedReason != "" || e.SubstateMessage != "" {
		t.Fatalf("PhaseEvent zero value should have empty string fields; got %+v", e)
	}
	if !e.Timestamp.IsZero() {
		t.Fatalf("PhaseEvent zero value should have zero Timestamp; got %v", e.Timestamp)
	}
}

// mockReader satisfies SourceReader for table-driven testing.
type mockReader struct {
	id     SourceID
	events []PhaseEvent
}

func (m *mockReader) ID() SourceID { return m.id }
func (m *mockReader) Stream(ctx context.Context, deployID string, out chan<- PhaseEvent) error {
	for _, e := range m.events {
		select {
		case out <- e:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// TestSourceReader_InterfaceContract proves the interface compiles + a
// minimal mock satisfies it. Foundation for adapter implementations T3.2-T3.6.
func TestSourceReader_InterfaceContract(t *testing.T) {
	var _ SourceReader = (*mockReader)(nil)
	r := &mockReader{
		id: SourceLokiMarkers,
		events: []PhaseEvent{
			{
				Phase:     string(statemachine.DTPhaseBuild),
				Status:    string(statemachine.DTStatusInProgress),
				Timestamp: time.Now(),
			},
		},
	}
	if r.ID() != SourceLokiMarkers {
		t.Fatalf("mockReader.ID() = %v; want %v", r.ID(), SourceLokiMarkers)
	}
}
