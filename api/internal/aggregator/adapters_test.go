// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package aggregator

import (
	"context"
	"sync"
	"testing"
	"time"
)

// drainN reads up to n events with timeout.
func drainN(t *testing.T, ch <-chan PhaseEvent, n int, timeout time.Duration) []PhaseEvent {
	t.Helper()
	out := make([]PhaseEvent, 0, n)
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for len(out) < n {
		select {
		case e := <-ch:
			out = append(out, e)
		case <-deadline.C:
			return out
		}
	}
	return out
}

// --- release_db --------------------------------------------------------

type fakeReleaseQ struct {
	mu     sync.Mutex
	rows   []*ReleaseStatusRow
	called int
}

func (f *fakeReleaseQ) QueryReleaseStatus(ctx context.Context, deployID string) (*ReleaseStatusRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.called >= len(f.rows) {
		return f.rows[len(f.rows)-1], nil
	}
	r := f.rows[f.called]
	f.called++
	return r, nil
}

func TestReleaseDBReader_PendingThenActive(t *testing.T) {
	q := &fakeReleaseQ{rows: []*ReleaseStatusRow{
		{State: "PENDING", UpdatedAt: time.Now()},
		{State: "ACTIVE", UpdatedAt: time.Now()},
		{State: "ACTIVE", UpdatedAt: time.Now()},
	}}
	r := NewReleaseDBReader(q)
	r.PollInterval = 10 * time.Millisecond
	out := make(chan PhaseEvent, 8)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() { _ = r.Stream(ctx, "dep-1", out); close(out) }()
	evs := drainN(t, out, 2, 500*time.Millisecond)
	if len(evs) < 2 {
		t.Fatalf("expected ≥2 events; got %d", len(evs))
	}
	if evs[0].Phase != "release" || evs[0].Status != "in_progress" {
		t.Errorf("evs[0] = %+v", evs[0])
	}
	if evs[1].Status != "completed" {
		t.Errorf("evs[1] = %+v", evs[1])
	}
}

func TestReleaseDBReader_FailedTerminal(t *testing.T) {
	q := &fakeReleaseQ{rows: []*ReleaseStatusRow{{State: "FAILED", UpdatedAt: time.Now()}}}
	r := NewReleaseDBReader(q)
	r.PollInterval = 10 * time.Millisecond
	out := make(chan PhaseEvent, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Stream(ctx, "dep-2", out) }()
	got := <-out
	if got.Status != "failed" {
		t.Errorf("status = %s; want failed", got.Status)
	}
	if err := <-done; err != nil {
		t.Errorf("Stream err = %v; want nil (terminal)", err)
	}
}

// --- loki_markers ------------------------------------------------------

type fakeLokiQ struct {
	markers []LokiMarker
	called  int
	mu      sync.Mutex
}

func (f *fakeLokiQ) QueryMarkersAfter(ctx context.Context, deployID string, afterTS time.Time) ([]LokiMarker, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.called > 0 {
		return nil, nil
	}
	f.called++
	return f.markers, nil
}

func TestLokiMarkersReader_StreamsBuildToLive(t *testing.T) {
	q := &fakeLokiQ{markers: []LokiMarker{
		{Phase: "build", Step: "clone", Level: "info", Timestamp: time.Now()},
		{Phase: "build", Step: "completed", Level: "info", Timestamp: time.Now()},
		{Phase: "live", Step: "ready", Level: "info", Timestamp: time.Now()},
	}}
	r := NewLokiMarkersReader(q)
	r.PollInterval = 10 * time.Millisecond
	out := make(chan PhaseEvent, 8)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Stream(ctx, "dep-3", out) }()
	evs := drainN(t, out, 3, 500*time.Millisecond)
	if len(evs) != 3 {
		t.Fatalf("expected 3 events; got %d", len(evs))
	}
	if evs[2].Phase != "live" || evs[2].Status != "completed" {
		t.Errorf("evs[2] = %+v; want live/completed", evs[2])
	}
}

// --- argocd_app --------------------------------------------------------

type fakeArgoQ struct {
	statuses []*ArgoCDAppStatus
	called   int
	mu       sync.Mutex
}

func (f *fakeArgoQ) QueryAppStatus(ctx context.Context, deployID string) (*ArgoCDAppStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.called >= len(f.statuses) {
		return f.statuses[len(f.statuses)-1], nil
	}
	st := f.statuses[f.called]
	f.called++
	return st, nil
}

func TestArgoCDAppReader_ProgressingToHealthy(t *testing.T) {
	q := &fakeArgoQ{statuses: []*ArgoCDAppStatus{
		{HealthStatus: "Progressing", SyncStatus: "OutOfSync", UpdatedAt: time.Now()},
		{HealthStatus: "Healthy", SyncStatus: "Synced", UpdatedAt: time.Now()},
	}}
	r := NewArgoCDAppReader(q)
	r.PollInterval = 10 * time.Millisecond
	out := make(chan PhaseEvent, 8)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Stream(ctx, "dep-4", out) }()
	evs := drainN(t, out, 2, 500*time.Millisecond)
	if len(evs) < 2 {
		t.Fatalf("expected 2 events; got %d", len(evs))
	}
	if evs[1].Phase != "deploy" || evs[1].Status != "completed" {
		t.Errorf("evs[1] = %+v; want deploy/completed", evs[1])
	}
}

func TestArgoCDAppReader_DegradedTerminal(t *testing.T) {
	q := &fakeArgoQ{statuses: []*ArgoCDAppStatus{{HealthStatus: "Degraded", SyncStatus: "Synced", UpdatedAt: time.Now()}}}
	r := NewArgoCDAppReader(q)
	r.PollInterval = 10 * time.Millisecond
	out := make(chan PhaseEvent, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	go func() { _ = r.Stream(ctx, "dep-5", out); close(out) }()
	got := drainN(t, out, 1, 200*time.Millisecond)
	if len(got) == 0 {
		t.Fatalf("no event received")
	}
	if got[0].Status != "failed" {
		t.Errorf("status = %s; want failed (Degraded health)", got[0].Status)
	}
}

// --- loopzeroapp_informer --------------------------------------------------

type fakeLoopZeroQ struct {
	phases []*LoopZeroAppPhase
	called int
	mu     sync.Mutex
}

func (f *fakeLoopZeroQ) QueryLoopZeroAppPhase(ctx context.Context, deployID string) (*LoopZeroAppPhase, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.called >= len(f.phases) {
		return f.phases[len(f.phases)-1], nil
	}
	p := f.phases[f.called]
	f.called++
	return p, nil
}

func TestLoopZeroAppInformerReader_ProgressingToReachable(t *testing.T) {
	q := &fakeLoopZeroQ{phases: []*LoopZeroAppPhase{
		{Phase: "PROGRESSING", UpdatedAt: time.Now()},
		{Phase: "REACHABLE", UpdatedAt: time.Now()},
	}}
	r := NewLoopZeroAppInformerReader(q)
	r.PollInterval = 10 * time.Millisecond
	out := make(chan PhaseEvent, 8)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() { _ = r.Stream(ctx, "dep-6", out); close(out) }()
	evs := drainN(t, out, 2, 500*time.Millisecond)
	if len(evs) < 2 {
		t.Fatalf("expected 2 events; got %d", len(evs))
	}
	if evs[1].Phase != "live" {
		t.Errorf("evs[1].Phase = %s; want live (REACHABLE→live mapping)", evs[1].Phase)
	}
}

// --- status_url_probe --------------------------------------------------

type fakeProber struct {
	results []*StatusURLProbeResult
	called  int
	mu      sync.Mutex
}

func (f *fakeProber) ProbeStatusURL(ctx context.Context, deployID string) (*StatusURLProbeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.called >= len(f.results) {
		return f.results[len(f.results)-1], nil
	}
	r := f.results[f.called]
	f.called++
	return r, nil
}

func TestStatusURLProbeReader_ReachableTerminal(t *testing.T) {
	p := &fakeProber{results: []*StatusURLProbeResult{
		{Reachable: false, StatusCode: 0, Reason: "connection refused"},
		{Reachable: true, StatusCode: 200, Reason: "OK"},
	}}
	r := NewStatusURLProbeReader(p)
	r.PollInterval = 10 * time.Millisecond
	out := make(chan PhaseEvent, 8)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Stream(ctx, "dep-7", out) }()
	evs := drainN(t, out, 2, 500*time.Millisecond)
	if len(evs) < 2 {
		t.Fatalf("expected 2 events; got %d", len(evs))
	}
	if evs[1].Status != "completed" {
		t.Errorf("evs[1] = %+v; want live/completed", evs[1])
	}
}

func TestStatusURLProbeReader_BudgetExhausted(t *testing.T) {
	p := &fakeProber{results: []*StatusURLProbeResult{
		{Reachable: false, StatusCode: 0, Reason: "connection refused"},
	}}
	r := NewStatusURLProbeReader(p)
	r.PollInterval = 5 * time.Millisecond
	r.MaxAttempts = 3
	out := make(chan PhaseEvent, 8)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Stream(ctx, "dep-8", out) }()
	evs := drainN(t, out, 3, 500*time.Millisecond)
	last := evs[len(evs)-1]
	if last.Status != "failed" {
		t.Errorf("last = %+v; want failed (budget exhausted)", last)
	}
}
