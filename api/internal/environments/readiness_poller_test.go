// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package environments

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type fakeReadinessSource struct {
	samples []ReplicasReadySample
	err     error
}

func (f fakeReadinessSource) ReplicasReady(context.Context) ([]ReplicasReadySample, error) {
	return f.samples, f.err
}

type recordedSnapshot struct {
	target        EnvironmentTargetKey
	replicasReady int32
	at            time.Time
}

type fakeSnapshotStore struct {
	missing  map[EnvironmentTargetKey]bool
	failures map[EnvironmentTargetKey]error
	recorded []recordedSnapshot
}

func (f *fakeSnapshotStore) RecordReplicasReady(_ context.Context, tenantID, projectID, env string, replicasReady int32, at time.Time) (bool, error) {
	key := EnvironmentTargetKey{TenantID: tenantID, ProjectID: projectID, Environment: env}
	if err := f.failures[key]; err != nil {
		return false, err
	}
	if f.missing[key] {
		return false, nil
	}
	f.recorded = append(f.recorded, recordedSnapshot{key, replicasReady, at})
	return true, nil
}

var (
	pollerNow = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	shopProd  = EnvironmentTargetKey{TenantID: "tn-acme", ProjectID: "shop", Environment: "production"}
	blogProd  = EnvironmentTargetKey{TenantID: "tn-acme", ProjectID: "blog", Environment: "production"}
)

func newTestPoller(source ReplicasReadySource, store ReadinessSnapshotStore) *ReadinessPoller {
	p := NewReadinessPoller(source, store, time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil)))
	p.now = func() time.Time { return pollerNow }
	return p
}

func TestReadinessPollerTick_RecordsEverySampleWithTheScrapeTime(t *testing.T) {
	store := &fakeSnapshotStore{}
	source := fakeReadinessSource{samples: []ReplicasReadySample{
		{Target: shopProd, ReplicasReady: 2},
		{Target: blogProd, ReplicasReady: 0},
	}}

	if err := newTestPoller(source, store).Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	want := []recordedSnapshot{{shopProd, 2, pollerNow}, {blogProd, 0, pollerNow}}
	if len(store.recorded) != len(want) || store.recorded[0] != want[0] || store.recorded[1] != want[1] {
		t.Errorf("recorded = %+v, want %+v", store.recorded, want)
	}
}

func TestReadinessPollerTick_KeepsEveryStoredSnapshotWhenTheScrapeFails(t *testing.T) {
	store := &fakeSnapshotStore{}
	cause := errors.New("prometheus unreachable")

	err := newTestPoller(fakeReadinessSource{err: cause}, store).Tick(context.Background())

	if !errors.Is(err, cause) {
		t.Fatalf("Tick err = %v, want it to wrap %v", err, cause)
	}
	if len(store.recorded) != 0 {
		t.Errorf("recorded = %+v, want nothing written on a failed scrape", store.recorded)
	}
}

func TestReadinessPollerTick_AttemptsEverySampleAndNamesEachFailedTarget(t *testing.T) {
	store := &fakeSnapshotStore{failures: map[EnvironmentTargetKey]error{shopProd: errors.New("pg down")}}
	source := fakeReadinessSource{samples: []ReplicasReadySample{
		{Target: shopProd, ReplicasReady: 2},
		{Target: blogProd, ReplicasReady: 1},
	}}

	err := newTestPoller(source, store).Tick(context.Background())

	if err == nil || !strings.Contains(err.Error(), "tn-acme/shop/production") {
		t.Fatalf("Tick err = %v, want an error naming tn-acme/shop/production", err)
	}
	if len(store.recorded) != 1 || store.recorded[0].target != blogProd {
		t.Errorf("recorded = %+v, want the healthy sample still written", store.recorded)
	}
}

func TestReadinessPollerTick_DoesNotFailOnASampleWithNoTargetRow(t *testing.T) {
	// A workload still running for an environment whose target row is gone is
	// reported (log + metric), not a write failure.
	store := &fakeSnapshotStore{missing: map[EnvironmentTargetKey]bool{shopProd: true}}
	source := fakeReadinessSource{samples: []ReplicasReadySample{{Target: shopProd, ReplicasReady: 1}}}

	if err := newTestPoller(source, store).Tick(context.Background()); err != nil {
		t.Fatalf("Tick = %v, want nil", err)
	}
}

func TestReadinessPollerStart_StopsWhenTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		newTestPoller(fakeReadinessSource{}, &fakeSnapshotStore{}).Start(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after the context was cancelled")
	}
}
