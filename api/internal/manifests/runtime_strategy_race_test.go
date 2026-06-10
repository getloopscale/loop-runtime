// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Plan v1.2 T4.2 — runtimeToStrategyTable concurrency safety.

package manifests

import (
	"sync"
	"testing"
)

// TestRuntimeToStrategy_ConcurrentSafe — 200 goroutines call mapRuntimeToStrategy
// in parallel. Combined with `go test -race`, this asserts no map-race regression
// can be reintroduced.
func TestRuntimeToStrategy_ConcurrentSafe(t *testing.T) {
	const goroutines = 200
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			runtime := "python"
			if idx%2 == 0 {
				runtime = "node"
			}
			if _, err := mapRuntimeToStrategy(runtime); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent lookup failed: %v", err)
	}
}
