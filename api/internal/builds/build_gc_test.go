// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"
)

// --- Test doubles for BuildGC ---

type fakeBuildGCStore struct {
	deletedBuilds   int64
	deletedWebhooks int64
	buildsErr       error
	webhooksErr     error
	calledMaxAge    time.Duration
	calledRetain    int
}

func (f *fakeBuildGCStore) DeleteOldBuilds(_ context.Context, maxAge time.Duration, retain int) (int64, error) {
	f.calledMaxAge = maxAge
	f.calledRetain = retain
	return f.deletedBuilds, f.buildsErr
}
func (f *fakeBuildGCStore) DeleteOldWebhookDeliveries(_ context.Context, _ time.Duration) (int64, error) {
	return f.deletedWebhooks, f.webhooksErr
}

// --- Tests ---

func TestBuildGC_Collect_DeletesOldBuilds(t *testing.T) {
	// Arrange
	store := &fakeBuildGCStore{deletedBuilds: 5, deletedWebhooks: 2}
	gc := NewBuildGC(store, slog.Default(), time.Hour, 30*24*time.Hour, 10)

	// Act
	gc.collect(context.Background())

	// Assert
	if store.calledMaxAge != 30*24*time.Hour {
		t.Errorf("expected maxAge 30 days, got %v", store.calledMaxAge)
	}
	if store.calledRetain != 10 {
		t.Errorf("expected retain 10, got %d", store.calledRetain)
	}
}

func TestBuildGC_Collect_ContinuesOnBuildError(t *testing.T) {
	// Arrange: builds fail but webhooks should still be attempted
	store := &fakeBuildGCStore{
		buildsErr:       fmt.Errorf("db connection lost"),
		deletedWebhooks: 5,
	}
	gc := NewBuildGC(store, slog.Default(), time.Hour, 30*24*time.Hour, 10)

	// Act: should not panic
	gc.collect(context.Background())
}

func TestBuildGC_Defaults(t *testing.T) {
	// Arrange: all zero values
	gc := NewBuildGC(&fakeBuildGCStore{}, slog.Default(), 0, 0, 0)

	// Assert defaults
	if gc.interval != 1*time.Hour {
		t.Errorf("expected default interval 1h, got %v", gc.interval)
	}
	if gc.maxAge != 30*24*time.Hour {
		t.Errorf("expected default maxAge 30 days, got %v", gc.maxAge)
	}
	if gc.retain != 10 {
		t.Errorf("expected default retain 10, got %d", gc.retain)
	}
}

func TestBuildGC_Collect_NothingToDelete(t *testing.T) {
	// Arrange: all zero counts, no errors
	store := &fakeBuildGCStore{}
	gc := NewBuildGC(store, slog.Default(), time.Hour, 30*24*time.Hour, 10)

	// Act: should not panic, should be a no-op
	gc.collect(context.Background())
}
