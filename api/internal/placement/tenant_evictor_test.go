// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package placement

import (
	"context"
	"errors"
	"testing"
)

type fakeLister struct {
	byPattern map[string][]string
	err       error
}

func (f *fakeLister) ListTenantIDsByPattern(_ context.Context, pattern string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byPattern[pattern], nil
}

func TestMatchTenants_ExplicitIDsOnly(t *testing.T) {
	ev := NewTenantEvictor(nil)
	got, err := ev.MatchTenants(context.Background(), "", []string{"tn-a", "tn-b", "tn-a", ""})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 2 || got[0] != "tn-a" || got[1] != "tn-b" {
		t.Fatalf("expected [tn-a tn-b], got %v", got)
	}
}

func TestMatchTenants_PatternOnly(t *testing.T) {
	f := &fakeLister{byPattern: map[string][]string{
		"tn-lt-%": {"tn-lt-1", "tn-lt-2"},
	}}
	ev := NewTenantEvictor(f)
	got, err := ev.MatchTenants(context.Background(), "tn-lt-%", nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 ids, got %d: %v", len(got), got)
	}
}

func TestMatchTenants_PatternAndIDsDeduped(t *testing.T) {
	f := &fakeLister{byPattern: map[string][]string{
		"tn-lt-%": {"tn-lt-1", "tn-lt-2"},
	}}
	ev := NewTenantEvictor(f)
	got, err := ev.MatchTenants(context.Background(), "tn-lt-%", []string{"tn-lt-1", "tn-manual"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 unique ids, got %d: %v", len(got), got)
	}
	seen := map[string]struct{}{}
	for _, id := range got {
		if _, dup := seen[id]; dup {
			t.Fatalf("dup id: %s", id)
		}
		seen[id] = struct{}{}
	}
}

func TestMatchTenants_PatternErrorSurfaces(t *testing.T) {
	f := &fakeLister{err: errors.New("pg down")}
	ev := NewTenantEvictor(f)
	_, err := ev.MatchTenants(context.Background(), "tn-lt-%", nil)
	if err == nil {
		t.Fatalf("expected err")
	}
}

func TestMatchTenants_NilListerIgnoresPattern(t *testing.T) {
	ev := NewTenantEvictor(nil)
	got, err := ev.MatchTenants(context.Background(), "tn-lt-%", []string{"tn-explicit"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 1 || got[0] != "tn-explicit" {
		t.Fatalf("want [tn-explicit], got %v", got)
	}
}

func TestEvictTenants_RejectsEmpty(t *testing.T) {
	ev := NewTenantEvictor(nil)
	_, err := ev.EvictTenants(context.Background(), nil)
	if err == nil {
		t.Fatalf("expected err on empty tenant list")
	}
}

type fakeDeleter struct {
	called   int
	received []string
	counts   TenantDeletionCounts
	err      error
}

func (f *fakeDeleter) DeleteTenantsFKOrdered(_ context.Context, tenantIDs []string) (TenantDeletionCounts, error) {
	f.called++
	f.received = tenantIDs
	return f.counts, f.err
}

func TestEvictTenants_WithDeleter_RunsCleanup(t *testing.T) {
	d := &fakeDeleter{counts: TenantDeletionCounts{Tenants: 3, Projects: 5}}
	ev := NewTenantEvictorWithDB(nil, d)
	result, err := ev.EvictTenants(context.Background(), []string{"tn-a", "tn-b", "tn-c"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if d.called != 1 {
		t.Fatalf("deleter should be called exactly once, got %d", d.called)
	}
	if len(d.received) != 3 {
		t.Fatalf("deleter should get 3 ids, got %d", len(d.received))
	}
	got := result.Database
	if got.Tenants != 3 || got.Projects != 5 {
		t.Fatalf("counts not surfaced: %+v", got)
	}
}

func TestEvictTenants_DeleterError_Surfaces(t *testing.T) {
	d := &fakeDeleter{err: errDeleterDown()}
	ev := NewTenantEvictorWithDB(nil, d)
	_, err := ev.EvictTenants(context.Background(), []string{"tn-a"})
	if err == nil {
		t.Fatalf("expected err from deleter to propagate")
	}
}

func TestEvictTenants_NilDeleter_SkipsCleanup(t *testing.T) {
	ev := NewTenantEvictorWithDB(nil, nil)
	result, err := ev.EvictTenants(context.Background(), []string{"tn-a"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	// counts default-zero when no deleter ran
	if result.Database.Tenants != 0 {
		t.Fatalf("counts should be zero without deleter")
	}
}

func errDeleterDown() error {
	return errors.New("deleter down")
}

type fakeK8sEvictor struct {
	called   int
	received []string
	counts   K8sEvictionCounts
	err      error
}

func (f *fakeK8sEvictor) EvictTenantsFromClusters(_ context.Context, tenantIDs []string) (K8sEvictionCounts, error) {
	f.called++
	f.received = tenantIDs
	return f.counts, f.err
}

func TestEvictTenants_Full_RunsK8sThenDB(t *testing.T) {
	k8sFake := &fakeK8sEvictor{counts: K8sEvictionCounts{MgmtCRsDeleted: 5, NamespacesFinalized: 3}}
	dbFake := &fakeDeleter{counts: TenantDeletionCounts{Tenants: 5}}
	ev := NewTenantEvictorFull(nil, dbFake, k8sFake)

	result, err := ev.EvictTenants(context.Background(), []string{"tn-a", "tn-b"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if k8sFake.called != 1 || dbFake.called != 1 {
		t.Fatalf("both should be called once; k8s=%d db=%d", k8sFake.called, dbFake.called)
	}
	if result.Clusters.MgmtCRsDeleted != 5 {
		t.Fatalf("k8s counts not surfaced: %+v", result.Clusters)
	}
	if result.Database.Tenants != 5 {
		t.Fatalf("db counts not surfaced: %+v", result.Database)
	}
}

func TestEvictTenants_K8sError_SkipsDBCleanup(t *testing.T) {
	// Critical invariant: if K8s teardown fails, DB must NOT be wiped.
	// Otherwise the cluster is left with CRs referencing non-existent rows.
	k8sFake := &fakeK8sEvictor{err: errors.New("apiserver down")}
	dbFake := &fakeDeleter{}
	ev := NewTenantEvictorFull(nil, dbFake, k8sFake)

	_, err := ev.EvictTenants(context.Background(), []string{"tn-a"})
	if err == nil {
		t.Fatalf("expected err to propagate")
	}
	if dbFake.called != 0 {
		t.Fatalf("DB cleanup must NOT run when K8s fails (called %d times)", dbFake.called)
	}
}

func TestEvictTenants_K8sOnly_NoDeleter(t *testing.T) {
	k8sFake := &fakeK8sEvictor{}
	ev := NewTenantEvictorFull(nil, nil, k8sFake)
	_, err := ev.EvictTenants(context.Background(), []string{"tn-a"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if k8sFake.called != 1 {
		t.Fatalf("k8s evictor should run when provided")
	}
}
