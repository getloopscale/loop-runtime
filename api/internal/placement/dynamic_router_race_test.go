// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package placement

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// Resolving an unknown cell while the cell list is being refreshed must not
// read the router's state outside its lock. Run under -race.
func TestDynamicRouter_ResolveUnknownCellIsSafeDuringRefresh(t *testing.T) {
	store := &fakeCellStore{cells: []CellConfig{{CellID: "cell-a"}, {CellID: "cell-b"}}}
	router := NewDynamicRouter(store, time.Minute, slog.Default())
	ctx := context.Background()
	if err := router.refresh(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = router.refresh(ctx)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			if _, err := router.Resolve(ctx, "cell-unknown"); err == nil {
				t.Error("unknown cell resolved")
				return
			}
		}
	}()
	wg.Wait()
}
