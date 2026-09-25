// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argocd

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSyncSerializer_AcquireApp_SerializesSameApp asserts that 5 goroutines
// AcquireApp("X") in parallel are serialized (sequential acquisition).
// Plan T1.1 RED #1.
func TestSyncSerializer_AcquireApp_SerializesSameApp(t *testing.T) {
	s := NewSyncSerializer()
	var counter int64
	var maxInflight int64
	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := s.AcquireApp(context.Background(), "X")
			if err != nil {
				t.Errorf("AcquireApp: %v", err)
				return
			}
			defer release()
			cur := atomic.AddInt64(&counter, 1)
			for {
				old := atomic.LoadInt64(&maxInflight)
				if cur > old {
					if atomic.CompareAndSwapInt64(&maxInflight, old, cur) {
						break
					}
				} else {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			atomic.AddInt64(&counter, -1)
		}()
	}
	wg.Wait()

	if maxInflight != 1 {
		t.Errorf("maxInflight for same App = %d, want 1 (serialized)", maxInflight)
	}
}

// TestSyncSerializer_AcquireApp_ParallelDifferentApps asserts 5 goroutines on
// DIFFERENT apps run concurrently (not serialized cross-App).
// Plan T1.1 RED #2.
func TestSyncSerializer_AcquireApp_ParallelDifferentApps(t *testing.T) {
	s := NewSyncSerializer()
	var counter int64
	var maxInflight int64
	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		appName := []string{"A", "B", "C", "D", "E"}[i]
		go func(name string) {
			defer wg.Done()
			release, err := s.AcquireApp(context.Background(), name)
			if err != nil {
				t.Errorf("AcquireApp(%s): %v", name, err)
				return
			}
			defer release()
			cur := atomic.AddInt64(&counter, 1)
			for {
				old := atomic.LoadInt64(&maxInflight)
				if cur > old {
					if atomic.CompareAndSwapInt64(&maxInflight, old, cur) {
						break
					}
				} else {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			atomic.AddInt64(&counter, -1)
		}(appName)
	}
	wg.Wait()

	if maxInflight < 2 {
		t.Errorf("maxInflight for different Apps = %d, want >=2 (parallel)", maxInflight)
	}
}

// TestSyncSerializer_AcquireApp_CtxCancelled asserts ctx cancel returns
// ctx.Err() without deadlock + WITHOUT background goroutine holding lock.
// EC-2 MUST FIX verification: avoid go-func-Lock pattern that causes
// background goroutine to acquire lock after ctx cancelled.
// Plan T1.1 RED #3.
func TestSyncSerializer_AcquireApp_CtxCancelled(t *testing.T) {
	s := NewSyncSerializer()

	// Acquire first to force second to wait
	release1, err := s.AcquireApp(context.Background(), "X")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	// Cancel ctx before second acquire completes
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := s.AcquireApp(ctx, "X")
		done <- err
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Errorf("expected ctx error, got nil")
		}
		if err != context.Canceled && err != context.DeadlineExceeded {
			t.Errorf("expected ctx error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("AcquireApp blocked too long after ctx cancel — deadlock")
	}

	// Critical EC-2 assertion: release lock, then try to acquire again immediately.
	// If background goroutine has acquired the lock (the go-func-Lock bug),
	// this third AcquireApp would deadlock.
	release1()

	ctxQuick, cancelQuick := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancelQuick()
	release2, err := s.AcquireApp(ctxQuick, "X")
	if err != nil {
		t.Fatalf("post-cancel acquire deadlocked or errored: %v (EC-2 regression)", err)
	}
	release2()
}

// TestSyncSerializer_AcquireApp_PanicSafe asserts panic in critical section
// still releases lock (no deadlock on subsequent acquires).
// Plan T1.1 RED #4.
func TestSyncSerializer_AcquireApp_PanicSafe(t *testing.T) {
	s := NewSyncSerializer()

	func() {
		defer func() {
			_ = recover()
		}()
		release, err := s.AcquireApp(context.Background(), "X")
		if err != nil {
			t.Fatalf("acquire: %v", err)
		}
		defer release()
		panic("simulated work failure")
	}()

	// If lock leaked, this acquire would deadlock
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	release, err := s.AcquireApp(ctx, "X")
	if err != nil {
		t.Fatalf("post-panic acquire: %v (lock leaked)", err)
	}
	release()
}

// TestSyncSerializer_OpportunisticEviction asserts that after release of an
// idle App, the lock is removed from the internal map (EC-1).
// Plan T1.1 RED #5.
func TestSyncSerializer_OpportunisticEviction(t *testing.T) {
	s := NewSyncSerializer()

	release, err := s.AcquireApp(context.Background(), "ephemeral-preview-pr-42")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	if !s.HasApp("ephemeral-preview-pr-42") {
		t.Errorf("expected app to be present after acquire")
	}

	release()

	// Allow eviction to complete
	time.Sleep(10 * time.Millisecond)

	if s.HasApp("ephemeral-preview-pr-42") {
		t.Errorf("expected app to be evicted after release (EC-1 unbounded growth prevention)")
	}
}

// TestSyncClient_TimeoutDefault asserts NewSyncClient with empty cfg uses
// the 90s default timeout (vs previous hardcoded 30s).
// Plan T1.1 RED #6.
func TestSyncClient_TimeoutDefault(t *testing.T) {
	c := mustNewSyncClient(t, SyncClientConfig{BaseURL: "http://localhost"})
	if c.client.Timeout != 90*time.Second {
		t.Errorf("default Timeout = %v, want 90s", c.client.Timeout)
	}
}

// TestSyncClient_TimeoutFromConfig asserts explicit cfg.Timeout overrides default.
// Plan T1.1 RED #7.
func TestSyncClient_TimeoutFromConfig(t *testing.T) {
	c := mustNewSyncClient(t, SyncClientConfig{BaseURL: "http://localhost", Timeout: 60 * time.Second})
	if c.client.Timeout != 60*time.Second {
		t.Errorf("configured Timeout = %v, want 60s", c.client.Timeout)
	}
}
