// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package domains

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"
)

// --- Test doubles for DomainCFReconciler ---

type fakeCFReconcilerStore struct {
	domains      []CFDomainInfo
	updatedZones map[string]string
	findErr      error
	updateErr    error
}

func newFakeCFReconcilerStore() *fakeCFReconcilerStore {
	return &fakeCFReconcilerStore{
		updatedZones: make(map[string]string),
	}
}

func (f *fakeCFReconcilerStore) FindActiveCFDomains(_ context.Context) ([]CFDomainInfo, error) {
	return f.domains, f.findErr
}
func (f *fakeCFReconcilerStore) UpdateCFZoneStatus(_ context.Context, domain, status string) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.updatedZones[domain] = status
	return nil
}

type fakeCFStatusReader struct {
	statuses map[string]string
	errs     map[string]error
}

func (f *fakeCFStatusReader) GetZoneStatus(_ context.Context, zoneID string) (string, error) {
	if err, ok := f.errs[zoneID]; ok {
		return "", err
	}
	return f.statuses[zoneID], nil
}

// --- Tests ---

func TestDomainCFReconciler_NoDrift_NoUpdate(t *testing.T) {
	store := newFakeCFReconcilerStore()
	store.domains = []CFDomainInfo{
		{Domain: "example.com", CFZoneID: "zone-1", CFZoneStatus: "active"},
	}

	cfReader := &fakeCFStatusReader{
		statuses: map[string]string{"zone-1": "active"},
	}

	reconciler := NewDomainCFReconciler(store, cfReader, slog.Default(), time.Minute)
	reconciler.reconcile(context.Background())

	if len(store.updatedZones) != 0 {
		t.Error("expected no updates when no drift")
	}
}

func TestDomainCFReconciler_DriftDetected_UpdatesStatus(t *testing.T) {
	store := newFakeCFReconcilerStore()
	store.domains = []CFDomainInfo{
		{Domain: "example.com", CFZoneID: "zone-1", CFZoneStatus: "active"},
	}

	cfReader := &fakeCFStatusReader{
		statuses: map[string]string{"zone-1": "moved"},
	}

	reconciler := NewDomainCFReconciler(store, cfReader, slog.Default(), time.Minute)
	reconciler.reconcile(context.Background())

	if store.updatedZones["example.com"] != "moved" {
		t.Errorf("expected zone status to be updated to 'moved', got %q", store.updatedZones["example.com"])
	}
}

func TestDomainCFReconciler_CFAPIError_SkipsDomain(t *testing.T) {
	store := newFakeCFReconcilerStore()
	store.domains = []CFDomainInfo{
		{Domain: "fail.com", CFZoneID: "zone-bad", CFZoneStatus: "active"},
		{Domain: "ok.com", CFZoneID: "zone-good", CFZoneStatus: "pending"},
	}

	cfReader := &fakeCFStatusReader{
		statuses: map[string]string{"zone-good": "active"},
		errs:     map[string]error{"zone-bad": fmt.Errorf("CF API timeout")},
	}

	reconciler := NewDomainCFReconciler(store, cfReader, slog.Default(), time.Minute)
	reconciler.reconcile(context.Background())

	// fail.com should be skipped (CF error), ok.com should be updated
	if _, ok := store.updatedZones["fail.com"]; ok {
		t.Error("failed domain should not be updated")
	}
	if store.updatedZones["ok.com"] != "active" {
		t.Errorf("expected ok.com updated to 'active', got %q", store.updatedZones["ok.com"])
	}
}

func TestDomainCFReconciler_StoreQueryError_ReturnsEarly(t *testing.T) {
	store := newFakeCFReconcilerStore()
	store.findErr = fmt.Errorf("database connection failed")

	cfReader := &fakeCFStatusReader{}

	reconciler := NewDomainCFReconciler(store, cfReader, slog.Default(), time.Minute)
	// Should not panic
	reconciler.reconcile(context.Background())
}

func TestDomainCFReconciler_ContextCancelled_StopsEarly(t *testing.T) {
	store := newFakeCFReconcilerStore()
	store.domains = []CFDomainInfo{
		{Domain: "a.com", CFZoneID: "z1", CFZoneStatus: "active"},
		{Domain: "b.com", CFZoneID: "z2", CFZoneStatus: "active"},
	}

	cfReader := &fakeCFStatusReader{
		statuses: map[string]string{"z1": "moved", "z2": "moved"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	reconciler := NewDomainCFReconciler(store, cfReader, slog.Default(), time.Minute)
	reconciler.reconcile(ctx)
	// Should not process any domains (context cancelled)
}

func TestNewDomainCFReconciler_DefaultInterval(t *testing.T) {
	r := NewDomainCFReconciler(nil, nil, slog.Default(), 0)
	if r.interval != 15*time.Minute {
		t.Errorf("expected default interval 15m, got %v", r.interval)
	}
}

func TestNewDomainCFReconciler_CustomInterval(t *testing.T) {
	r := NewDomainCFReconciler(nil, nil, slog.Default(), 5*time.Minute)
	if r.interval != 5*time.Minute {
		t.Errorf("expected interval 5m, got %v", r.interval)
	}
}

func TestDomainCFReconciler_MultipleDomains_DriftOnSome(t *testing.T) {
	store := newFakeCFReconcilerStore()
	store.domains = []CFDomainInfo{
		{Domain: "a.com", CFZoneID: "z1", CFZoneStatus: "active"},
		{Domain: "b.com", CFZoneID: "z2", CFZoneStatus: "active"},
		{Domain: "c.com", CFZoneID: "z3", CFZoneStatus: "pending"},
	}

	cfReader := &fakeCFStatusReader{
		statuses: map[string]string{
			"z1": "active",  // no drift
			"z2": "deleted", // drift
			"z3": "pending", // no drift
		},
	}

	reconciler := NewDomainCFReconciler(store, cfReader, slog.Default(), time.Minute)
	reconciler.reconcile(context.Background())

	if len(store.updatedZones) != 1 {
		t.Errorf("expected 1 update, got %d", len(store.updatedZones))
	}
	if store.updatedZones["b.com"] != "deleted" {
		t.Errorf("expected b.com updated to 'deleted', got %q", store.updatedZones["b.com"])
	}
}
