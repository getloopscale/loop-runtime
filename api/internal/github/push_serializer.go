// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package github

import "sync"

// PushSerializer prevents redundant concurrent auto-deploys for the same repo+branch.
// When two pushes arrive quickly for the same branch, only one should download/bundle/upload.
// The environment lock (advisory lock) handles build-level serialization, but this
// prevents wasted S3 uploads and source downloads.
// MEET-2026-04-09-044 P1.
type PushSerializer struct {
	mu    sync.Mutex
	locks map[string]struct{}
}

// NewPushSerializer creates a new serializer.
func NewPushSerializer() *PushSerializer {
	return &PushSerializer{locks: make(map[string]struct{})}
}

// TryAcquire attempts to acquire a per-repo+branch lock. Returns true if acquired.
func (s *PushSerializer) TryAcquire(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, held := s.locks[key]; held {
		return false
	}
	s.locks[key] = struct{}{}
	return true
}

// Release releases a per-repo+branch lock.
func (s *PushSerializer) Release(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.locks, key)
}
