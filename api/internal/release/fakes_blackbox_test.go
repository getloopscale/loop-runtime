// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release_test

import (
	"context"
	"encoding/json"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/sse"
)

func newFakeEventStoreOrch() *fakeEventStoreOrch {
	return &fakeEventStoreOrch{}
}

// fakeEventStoreOrch implements sse.EventStore.
type fakeEventStoreOrch struct {
	events   []sse.SSEEvent
	appendID int64
}

func (f *fakeEventStoreOrch) Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error) {
	f.appendID++
	f.events = append(f.events, sse.SSEEvent{
		ID:        f.appendID,
		BuildID:   buildID,
		EventType: eventType,
		Data:      data,
		CreatedAt: time.Now().UTC(),
	})
	return f.appendID, nil
}

func (f *fakeEventStoreOrch) GetAfter(ctx context.Context, buildID string, afterID int64) ([]sse.SSEEvent, error) {
	var result []sse.SSEEvent
	for _, e := range f.events {
		if e.BuildID == buildID && e.ID > afterID {
			result = append(result, e)
		}
	}
	return result, nil
}

func (f *fakeEventStoreOrch) Prune(ctx context.Context, olderThan time.Duration) (int64, error) {
	return 0, nil
}

// fakeDeleteLocker implements deploycontracts.EnvironmentLocker for delete tests.
// Executes fn immediately with nil tx (no real lock).
type fakeDeleteLocker struct {
	lockErr error
}

func (f *fakeDeleteLocker) WithLock(_ context.Context, _, _, _ string, fn func(tx db.DBTX) error) error {
	if f.lockErr != nil {
		return f.lockErr
	}
	return fn(nil)
}
