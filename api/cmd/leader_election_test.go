// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/leader"
)

// scriptedAcquirer answers Acquire and Heartbeat from queues and records
// Release, so the election can be driven step by step.
type scriptedAcquirer struct {
	mu         sync.Mutex
	acquires   []error // nil = lease granted
	heartbeats []error
	acquired   int
	released   []*leader.Lease
}

func (a *scriptedAcquirer) Acquire(context.Context, string, string, string) (*leader.Lease, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	err := leader.ErrNotLeader
	if len(a.acquires) > 0 {
		err, a.acquires = a.acquires[0], a.acquires[1:]
	}
	if err != nil {
		return nil, err
	}
	a.acquired++
	return &leader.Lease{Name: "loopzero-controller", GenerationID: int64(a.acquired)}, nil
}

func (a *scriptedAcquirer) Heartbeat(context.Context, *leader.Lease) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.heartbeats) == 0 {
		return nil
	}
	err := a.heartbeats[0]
	a.heartbeats = a.heartbeats[1:]
	return err
}

func (a *scriptedAcquirer) Release(_ context.Context, l *leader.Lease) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.released = append(a.released, l)
	return nil
}

// A fenced lease stops the loops it started — their context is cancelled —
// and the process re-enters the election, starting the loops again only once
// it holds a lease again.
func TestLoopsElection_AFenceStopsTheLoopsAndReentersTheElection(t *testing.T) {
	acq := &scriptedAcquirer{
		acquires:   []error{leader.ErrNotLeader, nil, nil},
		heartbeats: []error{nil, leader.ErrFenced},
	}
	tick := make(chan time.Time)
	e := &loopsElection{acquirer: acq, name: "loopzero-controller", cellID: "cell-0", holder: "pod-a", tick: tick, logger: discardLogger}

	started := make(chan context.Context, 2)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.run(ctx, func(c context.Context) { started <- c }); close(done) }()

	tick <- time.Time{} // retry after ErrNotLeader -> lease 1
	first := <-started
	tick <- time.Time{} // heartbeat ok
	tick <- time.Time{} // heartbeat fenced -> loops stop, lease 2 acquired
	<-first.Done()
	second := <-started
	if second.Err() != nil {
		t.Fatal("the loops restarted with an already-cancelled context")
	}

	cancel()
	<-done
	<-second.Done()
	if len(acq.released) != 1 || acq.released[0].GenerationID != 2 {
		t.Fatalf("released %v, want exactly the held lease (generation 2) on shutdown", acq.released)
	}
}

// Without the lease the loops never start.
func TestLoopsElection_NoLeaseNoLoops(t *testing.T) {
	acq := &scriptedAcquirer{} // always ErrNotLeader
	tick := make(chan time.Time)
	e := &loopsElection{acquirer: acq, name: "loopzero-controller", cellID: "cell-0", holder: "pod-b", tick: tick, logger: discardLogger}

	startedLoops := false
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.run(ctx, func(context.Context) { startedLoops = true }); close(done) }()
	tick <- time.Time{}
	tick <- time.Time{}
	cancel()
	<-done
	if startedLoops {
		t.Fatal("loops started without holding the lease")
	}
	if len(acq.released) != 0 {
		t.Fatal("released a lease it never held")
	}
}
