// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package placementadapter

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db/store"
)

type evictingTenantStore struct {
	counts store.TenantDeletionCounts
	err    error
}

func (s evictingTenantStore) ListTenantIDsByPattern(context.Context, string) ([]string, error) {
	return nil, nil
}

func (s evictingTenantStore) DeleteTenantsFKOrdered(context.Context, []string) (store.TenantDeletionCounts, error) {
	return s.counts, s.err
}

// Every table the eviction deletes from must be reported to the operator.
// Fills every store count by reflection and requires the same-named field of
// the placement counts to carry it, so a step added to the cascade cannot be
// dropped from the report silently (metering_cursor was).
func TestTenantDBDeleterAdapter_ReportsEveryDeletedTable(t *testing.T) {
	var counts store.TenantDeletionCounts
	cv := reflect.ValueOf(&counts).Elem()
	for i := 0; i < cv.NumField(); i++ {
		cv.Field(i).SetInt(int64(i + 1))
	}
	a := &TenantDBDeleterAdapter{Store: evictingTenantStore{counts: counts}}

	got, err := a.DeleteTenantsFKOrdered(context.Background(), []string{"tn-1"})
	if err != nil {
		t.Fatalf("DeleteTenantsFKOrdered: %v", err)
	}
	gv := reflect.ValueOf(got)
	for i := 0; i < cv.NumField(); i++ {
		name := cv.Type().Field(i).Name
		f := gv.FieldByName(name)
		if !f.IsValid() {
			t.Errorf("placement.TenantDeletionCounts has no %s; that step's deletions are never reported", name)
			continue
		}
		if f.Int() != cv.Field(i).Int() {
			t.Errorf("%s = %d, want %d", name, f.Int(), cv.Field(i).Int())
		}
	}
}

func TestTenantDBDeleterAdapter_PropagatesTheStoreError(t *testing.T) {
	storeErr := errors.New("step 8 (tenants): fk violation")
	a := &TenantDBDeleterAdapter{Store: evictingTenantStore{err: storeErr}}
	if _, err := a.DeleteTenantsFKOrdered(context.Background(), []string{"tn-1"}); !errors.Is(err, storeErr) {
		t.Fatalf("want the store error, got %v", err)
	}
}
