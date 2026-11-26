// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metering

// worker_test.go — the metering worker's per-tenant unit (plan
// metering-compute-loop T3.1). Fakes for every collaborator: no live cluster, no
// live Postgres. Proves the dispatch decision (same-period vs straddle), the SC-3
// read-failure short-circuit, and the unplaced-tenant skip.

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db/store"

	corev1 "k8s.io/api/core/v1"

	"github.com/getloopscale/loop-runtime/api/internal/leader"
)

// --- fakes ---

// fakeMeteringStore is mutex-guarded so the RunOnce -race test (concurrent
// meterTenant goroutines) exercises RunOnce's race-freedom, not a race in the
// test double's own counters.
type fakeMeteringStore struct {
	mu              sync.Mutex
	cell            string
	cellFound       bool
	cellErr         error
	cellErrByTenant map[string]error // per-tenant cell error (error-isolation test)
	seedCalls       int
	seedErr         error
	cursor          time.Time
	cursorFound     bool
	cursorErr       error
	tier            string
	tierErr         error
	computeCalls    int
	splitCalls      int
	advanced        bool
	delta           int64
	meterErr        error
	lastInput       store.MeterInput
}

func (f *fakeMeteringStore) GetCellForTenant(ctx context.Context, tenantID string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e := f.cellErrByTenant[tenantID]; e != nil {
		return "", false, e
	}
	return f.cell, f.cellFound, f.cellErr
}
func (f *fakeMeteringStore) SeedMeteringCursor(ctx context.Context, tenantID, metric, cellID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seedCalls++
	return f.seedErr
}
func (f *fakeMeteringStore) GetMeteringCursor(ctx context.Context, tenantID, metric, cellID string) (time.Time, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cursor, f.cursorFound, f.cursorErr
}
func (f *fakeMeteringStore) GetPlanTierForTenant(ctx context.Context, tenantID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tier, f.tierErr
}
func (f *fakeMeteringStore) MeterCompute(ctx context.Context, in store.MeterInput) (bool, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.computeCalls++
	f.lastInput = in
	return f.advanced, f.delta, f.meterErr
}
func (f *fakeMeteringStore) MeterComputeSplit(ctx context.Context, in store.MeterInput) (bool, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.splitCalls++
	f.lastInput = in
	return f.advanced, f.delta, f.meterErr
}

type fakeNamespaceLister struct {
	mu         sync.Mutex
	namespaces []string
	err        error
	calls      int
}

func (f *fakeNamespaceLister) NamespacesForTenant(ctx context.Context, tenantID string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.namespaces, f.err
}

type fakePodListerFactory struct {
	mu     sync.Mutex
	lister PodLister
	err    error
	calls  int
}

func (f *fakePodListerFactory) PodListerForCell(ctx context.Context, cellID string) (PodLister, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.lister, f.err
}

type fakeClock struct{ now time.Time }

func (c fakeClock) Now() time.Time { return c.now }

type fakeMetrics struct {
	mu                 sync.Mutex
	cellErrors         int
	advancedCalls      int
	heldCalls          int
	panicCalls         int
	tenantMeteredCalls int
	passDurations      []float64
	lastFreshness      float64
	freshnessCalls     int
}

func (m *fakeMetrics) CellError()       { m.mu.Lock(); m.cellErrors++; m.mu.Unlock() }
func (m *fakeMetrics) Advanced(_ int64) { m.mu.Lock(); m.advancedCalls++; m.mu.Unlock() }
func (m *fakeMetrics) Held()            { m.mu.Lock(); m.heldCalls++; m.mu.Unlock() }
func (m *fakeMetrics) Panic()           { m.mu.Lock(); m.panicCalls++; m.mu.Unlock() }
func (m *fakeMetrics) TenantMetered()   { m.mu.Lock(); m.tenantMeteredCalls++; m.mu.Unlock() }
func (m *fakeMetrics) PassDuration(s float64) {
	m.mu.Lock()
	m.passDurations = append(m.passDurations, s)
	m.mu.Unlock()
}
func (m *fakeMetrics) SecondsSinceLastSuccess(s float64) {
	m.mu.Lock()
	m.lastFreshness = s
	m.freshnessCalls++
	m.mu.Unlock()
}

// fakeTenantLister returns a fixed active-tenant list (or an error) for RunOnce.
type fakeTenantLister struct {
	tenants []string
	err     error
}

func (f *fakeTenantLister) ListActiveTenants(ctx context.Context) ([]string, error) {
	return f.tenants, f.err
}

// newTestWorker builds a Worker whose collaborators are all fakes, with one
// namespace serving one 500m pod (so collect succeeds unless the podLister is
// overridden to error).
func newTestWorker(store *fakeMeteringStore, factory *fakePodListerFactory, nsLister *fakeNamespaceLister, now time.Time, metrics *fakeMetrics) *Worker {
	return &Worker{
		Lister:     nil, // meterTenant does not enumerate; RunOnce (later) will
		Store:      store,
		Namespaces: nsLister,
		PodListers: factory,
		Clock:      fakeClock{now: now},
		Metrics:    metrics,
		Lease:      leader.Lease{Name: "compute-metering", CellID: "cell-0", GenerationID: 7},
		MaxDelta:   time.Hour,
	}
}

// TestMeterTenant_SameMonth_DispatchesMeterCompute — when the cursor (expected)
// and now() fall in the same UTC month, the worker dispatches to MeterCompute
// (the same-period writer), threads the lease fence + Σ request into MeterInput,
// and records Advanced on a cursor advance.
func TestMeterTenant_SameMonth_DispatchesMeterCompute(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	expected := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) // same month as now
	store := &fakeMeteringStore{cell: "cell-0", cellFound: true, cursor: expected, cursorFound: true, tier: "pro", advanced: true, delta: 42}
	metrics := &fakeMetrics{}
	w := newTestWorker(store,
		&fakePodListerFactory{lister: fakePodLister{byNS: map[string][]corev1.Pod{"ns1": {podWith("500m", "1Gi")}}}},
		&fakeNamespaceLister{namespaces: []string{"ns1"}}, now, metrics)

	if err := w.meterTenant(context.Background(), "tn-a"); err != nil {
		t.Fatalf("meterTenant: %v", err)
	}
	if store.computeCalls != 1 || store.splitCalls != 0 {
		t.Fatalf("dispatch: compute=%d split=%d, want compute=1 split=0 (same month)", store.computeCalls, store.splitCalls)
	}
	if metrics.advancedCalls != 1 {
		t.Errorf("advanced metric=%d, want 1", metrics.advancedCalls)
	}
	if metrics.tenantMeteredCalls != 1 {
		t.Errorf("tenant_metered metric=%d, want 1 (a clean pass counts the tenant — subset-miss detector)", metrics.tenantMeteredCalls)
	}
	if store.lastInput.Gen != 7 || store.lastInput.ReqMillicore != 500 || store.lastInput.CellID != "cell-0" || store.lastInput.LoopName != "compute-metering" {
		t.Errorf("MeterInput = %+v, want Gen=7 ReqMillicore=500 CellID=cell-0 LoopName=compute-metering", store.lastInput)
	}
}

// TestMeterTenant_DifferentMonth_DispatchesMeterComputeSplit — when expected and
// now() straddle a month boundary, the worker dispatches to MeterComputeSplit
// (the period-rollover writer). BOTH dispatch paths are wired; neither is orphan
// money-code.
func TestMeterTenant_DifferentMonth_DispatchesMeterComputeSplit(t *testing.T) {
	now := time.Date(2026, 7, 1, 0, 30, 0, 0, time.UTC)
	expected := time.Date(2026, 6, 30, 23, 59, 0, 0, time.UTC) // previous month
	store := &fakeMeteringStore{cell: "cell-0", cellFound: true, cursor: expected, cursorFound: true, tier: "pro", advanced: true, delta: 10}
	metrics := &fakeMetrics{}
	w := newTestWorker(store,
		&fakePodListerFactory{lister: fakePodLister{byNS: map[string][]corev1.Pod{"ns1": {podWith("500m", "1Gi")}}}},
		&fakeNamespaceLister{namespaces: []string{"ns1"}}, now, metrics)

	if err := w.meterTenant(context.Background(), "tn-a"); err != nil {
		t.Fatalf("meterTenant: %v", err)
	}
	if store.splitCalls != 1 || store.computeCalls != 0 {
		t.Fatalf("dispatch: compute=%d split=%d, want compute=0 split=1 (month straddle)", store.computeCalls, store.splitCalls)
	}
}

// TestMeterTenant_CollectError_SC3_NoMeterNoSeed — SC-3: a pod-list failure
// short-circuits with a typed error BEFORE any seed or meter (the cursor is
// HELD, retried next tick — a read failure is NEVER swallowed to Σreq=0). A
// cell error is recorded.
func TestMeterTenant_CollectError_SC3_NoMeterNoSeed(t *testing.T) {
	boom := errors.New("cell unreachable")
	store := &fakeMeteringStore{cell: "cell-0", cellFound: true, cursorFound: true}
	metrics := &fakeMetrics{}
	w := newTestWorker(store,
		&fakePodListerFactory{lister: fakePodLister{errNS: map[string]error{"ns1": boom}}},
		&fakeNamespaceLister{namespaces: []string{"ns1"}},
		time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC), metrics)

	err := w.meterTenant(context.Background(), "tn-a")
	if err == nil {
		t.Fatal("meterTenant returned nil on a collect failure — SC-3 violated (must short-circuit)")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error must wrap the collect failure, got %v", err)
	}
	if store.computeCalls != 0 || store.splitCalls != 0 {
		t.Errorf("SC-3 violated: metered despite collect failure (compute=%d split=%d)", store.computeCalls, store.splitCalls)
	}
	if store.seedCalls != 0 {
		t.Errorf("SC-3 violated: seeded despite collect failure — the cursor must be untouched (seedCalls=%d)", store.seedCalls)
	}
	if metrics.cellErrors != 1 {
		t.Errorf("cellErrors=%d, want 1", metrics.cellErrors)
	}
	if metrics.tenantMeteredCalls != 0 {
		t.Errorf("tenant_metered=%d, want 0 — a tenant that ERRORED must NOT count as metered (excluded from subset detector)", metrics.tenantMeteredCalls)
	}
}

// TestMeterTenant_CellNotFound_SkipsWithoutError — a tenant with no canonical PG
// placement is skipped: no enumeration, no pod list, no seed, no meter, no cell
// error (nothing failed — it is a harmless skip, not an error path).
func TestMeterTenant_CellNotFound_SkipsWithoutError(t *testing.T) {
	store := &fakeMeteringStore{cellFound: false}
	metrics := &fakeMetrics{}
	nsLister := &fakeNamespaceLister{namespaces: []string{"ns1"}}
	factory := &fakePodListerFactory{lister: fakePodLister{}}
	w := newTestWorker(store, factory, nsLister, time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC), metrics)

	if err := w.meterTenant(context.Background(), "tn-a"); err != nil {
		t.Fatalf("an unplaced tenant must skip without error, got %v", err)
	}
	if nsLister.calls != 0 || factory.calls != 0 {
		t.Errorf("skip must not enumerate/list (namespaceCalls=%d factoryCalls=%d)", nsLister.calls, factory.calls)
	}
	if store.computeCalls != 0 || store.splitCalls != 0 || store.seedCalls != 0 {
		t.Errorf("skip must not meter/seed (compute=%d split=%d seed=%d)", store.computeCalls, store.splitCalls, store.seedCalls)
	}
	if metrics.cellErrors != 0 {
		t.Errorf("an unplaced-tenant skip is not a cell error, got cellErrors=%d", metrics.cellErrors)
	}
}

// TestWorker_RunOnce_MetersAllTenants_IsolatesErrors — RunOnce meters every
// active tenant; one tenant's failure is logged + surfaced in the aggregate
// error but NEVER aborts the pass — the healthy tenants are still metered (SC-3
// per-tenant HOLD isolation; reaper tradition).
func TestWorker_RunOnce_MetersAllTenants_IsolatesErrors(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	expected := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	boom := errors.New("cell lookup failed")
	store := &fakeMeteringStore{
		cell: "cell-0", cellFound: true, cursor: expected, cursorFound: true, tier: "pro", advanced: true, delta: 1,
		cellErrByTenant: map[string]error{"tn-bad": boom},
	}
	metrics := &fakeMetrics{}
	w := newTestWorker(store,
		&fakePodListerFactory{lister: fakePodLister{byNS: map[string][]corev1.Pod{"ns1": {podWith("500m", "1Gi")}}}},
		&fakeNamespaceLister{namespaces: []string{"ns1"}}, now, metrics)
	w.Lister = &fakeTenantLister{tenants: []string{"tn-ok1", "tn-bad", "tn-ok2"}}

	err := w.RunOnce(context.Background())
	if err == nil {
		t.Fatal("RunOnce returned nil despite a failing tenant — the pass must surface failures")
	}
	if !errors.Is(err, boom) {
		t.Errorf("aggregate error must contain the failing tenant's error, got %v", err)
	}
	if store.computeCalls != 2 {
		t.Errorf("computeCalls=%d, want 2 (both healthy tenants metered despite tn-bad failing)", store.computeCalls)
	}
}

// TestWorker_RunOnce_ParallelCellsNoRace — RunOnce fans out across tenants with
// bounded concurrency (plan T3.1 "bounded-concurrency writer"). Run with -race:
// no data race across the concurrent meterTenant goroutines, and every tenant is
// metered exactly once.
func TestWorker_RunOnce_ParallelCellsNoRace(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	expected := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	store := &fakeMeteringStore{cell: "cell-0", cellFound: true, cursor: expected, cursorFound: true, tier: "pro", advanced: true, delta: 1}
	metrics := &fakeMetrics{}
	w := newTestWorker(store,
		&fakePodListerFactory{lister: fakePodLister{byNS: map[string][]corev1.Pod{"ns1": {podWith("500m", "1Gi")}}}},
		&fakeNamespaceLister{namespaces: []string{"ns1"}}, now, metrics)
	w.MaxConcurrency = 4

	tenants := make([]string, 24)
	for i := range tenants {
		tenants[i] = "tn-" + strconv.Itoa(i)
	}
	w.Lister = &fakeTenantLister{tenants: tenants}

	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if store.computeCalls != len(tenants) {
		t.Errorf("computeCalls=%d, want %d (every tenant metered exactly once)", store.computeCalls, len(tenants))
	}
	if metrics.advancedCalls != len(tenants) {
		t.Errorf("advanced=%d, want %d", metrics.advancedCalls, len(tenants))
	}
}
