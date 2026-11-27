// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package domains

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

// Every status change the domain services make goes through the Domain state
// machine and is applied as a compare-and-set from the status the service read.

func transitionsOf(pairs []struct{ Domain, From, To string }) string { return fmt.Sprint(pairs) }

func TestDomainHealthChecker_LostCNAMERemovesTheDomainAsACompareAndSetFromActive(t *testing.T) {
	store := newFakeDomainHealthStore()
	store.activeDomains = []ports.CustomDomain{{
		Domain: "gone.invalid", TenantID: "tn-1", Status: "active", CNAMEConsecutiveFailures: 5,
	}}
	checker := NewDomainHealthChecker(store, slog.Default(), DomainHealthCheckerConfig{
		Interval: time.Second, CNAMETarget: "cname.example.com",
	})

	checker.checkCNAME(context.Background())

	if want := "[{gone.invalid active removing}]"; transitionsOf(store.transitions) != want {
		t.Errorf("transitions = %v, want %s", store.transitions, want)
	}
}

func TestDomainHealthChecker_StalePendingIsRemovedAsACompareAndSet(t *testing.T) {
	store := newFakeDomainHealthStore()
	store.stalePending = []ports.CustomDomain{{Domain: "stale.com", TenantID: "tn-1", Status: "pending_verification"}}
	checker := NewDomainHealthChecker(store, slog.Default(), DomainHealthCheckerConfig{Interval: time.Second, StaleDays: 7})

	checker.cleanupStalePending(context.Background())

	if want := "[{stale.com pending_verification removing}]"; transitionsOf(store.transitions) != want {
		t.Errorf("transitions = %v, want %s", store.transitions, want)
	}
}

// The row was read as provisioning and has moved on since (a concurrent delete
// or activation). The machine has no timeout edge from where it is now, so the
// checker must not overwrite it with 'failed'.
func TestDomainHealthChecker_ProvisioningTimeoutSkipsADomainNoLongerProvisioning(t *testing.T) {
	store := newFakeDomainHealthStore()
	store.staleProvisioning = []ports.CustomDomain{{Domain: "moved.com", TenantID: "tn-1", Status: "removing"}}
	checker := NewDomainHealthChecker(store, slog.Default(), DomainHealthCheckerConfig{Interval: time.Second})

	checker.checkProvisioningTimeout(context.Background())

	if _, written := store.failedDomains["moved.com"]; written {
		t.Error("a domain whose status has no timeout transition must not be marked failed")
	}
}

func TestDomainProvisioner_NSTimeoutSkipsADomainNoLongerPendingNS(t *testing.T) {
	store := newFakeDomainProvisionerStore()
	zone := "zone-1"
	store.pendingNSDomains = []ports.CustomDomain{{
		Domain: "late.com", TenantID: "tn-1", Status: "removing", CFZoneID: &zone,
		CreatedAt: time.Now().Add(-72 * time.Hour),
	}}
	provisioner := NewDomainProvisioner(store, &fakeDomainReleaseReader{}, &fakeDomainBuildReader{},
		&fakeGitOpsProvisioner{}, &fakeLockerProvisioner{}, slog.Default(), DomainProvisionerConfig{}).
		WithCFZone(&fakeCFZoneChecker{})

	provisioner.processPendingNS(context.Background())

	if _, written := store.updatedFailures["late.com"]; written {
		t.Error("a domain whose status has no NS-timeout transition must not be marked failed")
	}
}
