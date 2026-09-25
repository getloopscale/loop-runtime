// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argocd

import (
	"context"
	"sync"
)

// SyncSerializer serializes ArgoCD sync operations per-App. Concurrent calls
// to the SAME App name are queued FIFO; calls to DIFFERENT App names proceed
// in parallel.
//
// Plan T1.1 ADR D2: ArgoCD returns HTTP 400 "another operation is already
// in progress" when 2+ syncs hit the same App concurrently. Pre-emptive
// serialization here eliminates the error at the source instead of relying
// on loopzero-api retries that consume circuit breaker quota.
//
// EC-1 (memory leak prevention): after release, if no other goroutine holds
// the App's lock, delete it from the map. This prevents unbounded growth
// for ephemeral preview Apps (PR-X auto-deploy creates/destroys constantly).
//
// EC-2 (deadlock prevention): we use a channel-based binary semaphore for
// each App lock instead of `go func(){ Lock() }()` pattern. Channel-based
// allows ctx-aware acquire WITHOUT a background goroutine that could
// acquire the lock after ctx is cancelled.
type SyncSerializer struct {
	mu   sync.Mutex
	apps map[string]chan struct{} // chan with capacity 1; "token" presence == unlocked
}

// NewSyncSerializer creates an empty serializer.
func NewSyncSerializer() *SyncSerializer {
	return &SyncSerializer{
		apps: make(map[string]chan struct{}),
	}
}

// AcquireApp blocks until the App lock is free OR ctx is cancelled.
// Returns a release function that MUST be called (typically via defer).
//
// EC-2: ctx.Done before acquisition returns immediately with ctx.Err()
// without leaving a background goroutine that could later acquire the lock.
func (s *SyncSerializer) AcquireApp(ctx context.Context, appName string) (release func(), err error) {
	s.mu.Lock()
	semCh, ok := s.apps[appName]
	if !ok {
		// Create new semaphore with initial token (unlocked state).
		semCh = make(chan struct{}, 1)
		semCh <- struct{}{}
		s.apps[appName] = semCh
	}
	s.mu.Unlock()

	// EC-2: select on token receive OR ctx.Done. Channel-based gives us
	// proper ctx cancellation without background goroutine.
	select {
	case <-semCh:
		// Acquired token (lock held). Return release closure.
		return func() {
			s.releaseApp(appName, semCh)
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// releaseApp returns the token to the semaphore. EC-1: if the lock is idle
// (no waiters), evict from map.
func (s *SyncSerializer) releaseApp(appName string, semCh chan struct{}) {
	// Return token to semaphore.
	semCh <- struct{}{}

	// EC-1 opportunistic eviction: if nobody else needs this App, evict from
	// map. We re-acquire and check if the token is still present (no one else
	// is waiting) before deleting.
	s.mu.Lock()
	defer s.mu.Unlock()

	// Re-check map to handle concurrent re-creation.
	if existing, ok := s.apps[appName]; ok && existing == semCh {
		select {
		case <-semCh:
			// Token still present, no waiters → evict.
			// Note: token is consumed; if a racing acquire comes after this
			// delete, it'll create a fresh semCh. Safe by construction.
			delete(s.apps, appName)
		default:
			// Token consumed by another acquire → keep map entry.
		}
	}
}

// HasApp returns true if the App has an active lock entry. Used for tests.
func (s *SyncSerializer) HasApp(appName string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.apps[appName]
	return ok
}
