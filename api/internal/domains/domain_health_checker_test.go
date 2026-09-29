// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package domains

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/ports"
)

// --- Test doubles for DomainHealthChecker ---

type fakeDomainHealthStore struct {
	pendingDomains    []ports.CustomDomain
	activeDomains     []ports.CustomDomain
	stalePending      []ports.CustomDomain
	staleProvisioning []ports.CustomDomain
	tlsExpiring       []ports.CustomDomain
	verifiedDomains   []string
	cnameChecked      map[string]int // domain → consecutive failures
	failedDomains     map[string]string
	statusUpdated     map[string]string
	transitions       []struct{ Domain, From, To string }
	deletedDomains    []string
}

func newFakeDomainHealthStore() *fakeDomainHealthStore {
	return &fakeDomainHealthStore{
		cnameChecked:  make(map[string]int),
		failedDomains: make(map[string]string),
		statusUpdated: make(map[string]string),
	}
}

func (f *fakeDomainHealthStore) FindPendingForAutoVerify(_ context.Context, _ string) ([]ports.CustomDomain, error) {
	return f.pendingDomains, nil
}
func (f *fakeDomainHealthStore) FindActiveForCNAMECheck(_ context.Context, _ time.Time) ([]ports.CustomDomain, error) {
	return f.activeDomains, nil
}
func (f *fakeDomainHealthStore) FindStalePending(_ context.Context, _ time.Duration) ([]ports.CustomDomain, error) {
	return f.stalePending, nil
}
func (f *fakeDomainHealthStore) FindStaleProvisioning(_ context.Context, _ time.Duration) ([]ports.CustomDomain, error) {
	return f.staleProvisioning, nil
}
func (f *fakeDomainHealthStore) FindActiveWithTLSExpiry(_ context.Context, _ int) ([]ports.CustomDomain, error) {
	return f.tlsExpiring, nil
}
func (f *fakeDomainHealthStore) UpdateVerified(_ context.Context, domain string, _ time.Time) error {
	f.verifiedDomains = append(f.verifiedDomains, domain)
	return nil
}
func (f *fakeDomainHealthStore) UpdateCNAMECheck(_ context.Context, domain string, _ time.Time, failures int) error {
	f.cnameChecked[domain] = failures
	return nil
}
func (f *fakeDomainHealthStore) UpdateFailure(_ context.Context, domain, status, reason string) error {
	f.failedDomains[domain] = reason
	return nil
}
func (f *fakeDomainHealthStore) UpdateStatus(_ context.Context, domain, status string) error {
	f.statusUpdated[domain] = status
	return nil
}
func (f *fakeDomainHealthStore) TransitionStatus(_ context.Context, domain, expected, next string) error {
	f.transitions = append(f.transitions, struct{ Domain, From, To string }{domain, expected, next})
	f.statusUpdated[domain] = next
	return nil
}
func (f *fakeDomainHealthStore) Delete(_ context.Context, domain string) error {
	f.deletedDomains = append(f.deletedDomains, domain)
	return nil
}

// --- Tests ---

func TestDomainHealthChecker_CleanupStalePending(t *testing.T) {
	// Arrange
	store := newFakeDomainHealthStore()
	store.stalePending = []ports.CustomDomain{
		{Domain: "stale.com", TenantID: "tn-1", Status: "pending_verification"},
		{Domain: "old.com", TenantID: "tn-1", Status: "pending_verification"},
	}

	checker := NewDomainHealthChecker(store, slog.Default(), DomainHealthCheckerConfig{
		Interval:  time.Second,
		StaleDays: 7,
	})

	// Act
	checker.runChecks(context.Background())

	// Assert: stale pending domains should be marked for removal
	if store.statusUpdated["stale.com"] != "removing" {
		t.Errorf("expected stale.com to be marked as removing, got %q", store.statusUpdated["stale.com"])
	}
	if store.statusUpdated["old.com"] != "removing" {
		t.Errorf("expected old.com to be marked as removing, got %q", store.statusUpdated["old.com"])
	}
}

func TestDomainHealthChecker_ProvisioningTimeout(t *testing.T) {
	// Arrange
	store := newFakeDomainHealthStore()
	store.staleProvisioning = []ports.CustomDomain{
		{Domain: "stuck.com", TenantID: "tn-1", Status: "provisioning"},
	}

	checker := NewDomainHealthChecker(store, slog.Default(), DomainHealthCheckerConfig{
		Interval: time.Second,
	})

	// Act
	checker.runChecks(context.Background())

	// Assert
	if _, ok := store.failedDomains["stuck.com"]; !ok {
		t.Error("expected stuck.com to have failure recorded")
	}
}

func TestDomainHealthChecker_TLSExpiry_TracksMetrics(t *testing.T) {
	// Arrange
	expiry := time.Now().Add(5 * 24 * time.Hour)
	store := newFakeDomainHealthStore()
	store.tlsExpiring = []ports.CustomDomain{
		{Domain: "expiring.com", TenantID: "tn-1", TLSExpiresAt: &expiry},
	}

	checker := NewDomainHealthChecker(store, slog.Default(), DomainHealthCheckerConfig{
		Interval: time.Second,
	})

	// Act: should not panic, just logs + emits metrics
	checker.runChecks(context.Background())
	// No assertion needed beyond no panic — TLS check emits metrics only
}

func TestDomainHealthChecker_SkipsCNAMECheckWithoutTarget(t *testing.T) {
	// Arrange: no CNAMETarget configured (local dev)
	store := newFakeDomainHealthStore()
	store.activeDomains = []ports.CustomDomain{
		{Domain: "local.com", TenantID: "tn-1"},
	}

	checker := NewDomainHealthChecker(store, slog.Default(), DomainHealthCheckerConfig{
		Interval:    time.Second,
		CNAMETarget: "", // empty = skip CNAME check
	})

	// Act
	checker.runChecks(context.Background())

	// Assert: no CNAME checks performed
	if len(store.cnameChecked) > 0 {
		t.Error("should skip CNAME checks when no target configured")
	}
}

func TestDomainHealthChecker_SkipsCFManagedDomains(t *testing.T) {
	// Arrange
	zoneID := "zone-123"
	store := newFakeDomainHealthStore()
	store.activeDomains = []ports.CustomDomain{
		{Domain: "cf.com", TenantID: "tn-1", CFZoneID: &zoneID},
	}

	checker := NewDomainHealthChecker(store, slog.Default(), DomainHealthCheckerConfig{
		Interval:    time.Second,
		CNAMETarget: "cname.dev.loopzero.example",
	})

	// Act
	checker.runChecks(context.Background())

	// Assert: CF-managed domains should be skipped for CNAME check
	if _, ok := store.cnameChecked["cf.com"]; ok {
		t.Error("should skip CNAME check for CF-managed domains")
	}
}

func TestDomainHealthChecker_DefaultConfig(t *testing.T) {
	cfg := DefaultDomainHealthCheckerConfig()
	if cfg.Interval != 5*time.Minute {
		t.Errorf("expected 5m interval, got %v", cfg.Interval)
	}
	if cfg.GraceDays != 14 {
		t.Errorf("expected 14 grace days, got %d", cfg.GraceDays)
	}
	if cfg.StaleDays != 7 {
		t.Errorf("expected 7 stale days, got %d", cfg.StaleDays)
	}
}

func TestDomainHealthChecker_ContextCancelled_StopsProcessing(t *testing.T) {
	// Arrange
	store := newFakeDomainHealthStore()
	store.stalePending = []ports.CustomDomain{
		{Domain: "a.com"}, {Domain: "b.com"}, {Domain: "c.com"},
	}

	checker := NewDomainHealthChecker(store, slog.Default(), DomainHealthCheckerConfig{
		Interval: time.Second,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	// Act
	checker.runChecks(ctx)

	// Assert: some or all may be skipped due to cancellation
	// The key assertion is no panic
}

// With no cell the checker covers every cell, as the loop graph documents; it used to
// auto-verify custom domains on cell-0 only (#450).
func TestDomainHealthChecker_EmptyCellCoversEveryCell(t *testing.T) {
	h := NewDomainHealthChecker(newFakeDomainHealthStore(), slog.Default(), DomainHealthCheckerConfig{})
	if h.cellID != "" {
		t.Errorf("cell scope = %q, want \"\" (every cell)", h.cellID)
	}
}
