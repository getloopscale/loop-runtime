// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// T2.1 tests for DataPlanePoller — plan dashboard-badge-data-plane-poller.
// Given a fake PrometheusInstantClient + sqlmock DB, When Tick fires,
// Then env_targets rows are updated with values and timestamps; scrape
// errors preserve last-known-good; leader-election flag is true (EC-6).

package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/prometheus/common/model"
)

// fakePromClient returns a fixed vector OR error. When err is non-nil,
// Query returns it before touching vec (preserve-last-known-good path).
type fakePromClient struct {
	vec model.Vector
	err error
}

func (f *fakePromClient) Query(ctx context.Context, q string, t time.Time) (model.Vector, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.vec, nil
}

// TestDataPlanePoller_TickWritesReplicasReadySnapshot — Given a fake
// Prometheus vector with 2 samples (tenant/project/env labels + value),
// When Tick fires, Then the 2 corresponding env_targets rows are
// UPDATE-ed with matching value + recent timestamp.
func TestDataPlanePoller_TickWritesReplicasReadySnapshot(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	vec := model.Vector{
		{
			Metric: model.Metric{
				"label_loopzero_io_tenant":      "tn-alpha",
				"label_loopzero_io_project":     "proj-one",
				"label_loopzero_io_environment": "production",
			},
			Value: 3,
		},
		{
			Metric: model.Metric{
				"label_loopzero_io_tenant":      "tn-beta",
				"label_loopzero_io_project":     "proj-two",
				"label_loopzero_io_environment": "production",
			},
			Value: 0,
		},
	}
	prom := &fakePromClient{vec: vec}

	mock.ExpectExec("UPDATE environment_targets").
		WithArgs(int64(3), sqlmock.AnyArg(), "tn-alpha", "proj-one", "production").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE environment_targets").
		WithArgs(int64(0), sqlmock.AnyArg(), "tn-beta", "proj-two", "production").
		WillReturnResult(sqlmock.NewResult(0, 1))

	p := NewDataPlanePoller(prom, db, 30*time.Second, nil)
	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("Tick returned err: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// TestDataPlanePoller_ScrapeErrorPreservesLastKnownGood — Given a fake
// Prometheus client returning an error, When Tick fires, Then NO
// UPDATE runs (env_targets rows untouched) and Tick returns the error.
func TestDataPlanePoller_ScrapeErrorPreservesLastKnownGood(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	prom := &fakePromClient{err: errors.New("prometheus unreachable")}
	// No ExpectExec — the sqlmock will fail if any UPDATE fires.

	p := NewDataPlanePoller(prom, db, 30*time.Second, nil)
	err = p.Tick(context.Background())
	if err == nil {
		t.Fatalf("Tick should have returned scrape error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations (unexpected UPDATE fired): %v", err)
	}
}

// TestDataPlanePoller_SkipsSamplesMissingCanonicalLabels — Given a fake
// Prometheus vector where one sample is missing loopzero.io/project label,
// When Tick fires, Then only the sample with all 3 labels drives an
// UPDATE (defensive against non-LoopZero Deployments in loopzero-* namespaces).
func TestDataPlanePoller_SkipsSamplesMissingCanonicalLabels(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	vec := model.Vector{
		{
			Metric: model.Metric{
				"label_loopzero_io_tenant":      "tn-alpha",
				"label_loopzero_io_project":     "proj-one",
				"label_loopzero_io_environment": "production",
			},
			Value: 2,
		},
		{
			// missing label_loopzero_io_project — must be skipped
			Metric: model.Metric{
				"label_loopzero_io_tenant":      "tn-gamma",
				"label_loopzero_io_environment": "staging",
			},
			Value: 5,
		},
	}
	prom := &fakePromClient{vec: vec}

	mock.ExpectExec("UPDATE environment_targets").
		WithArgs(int64(2), sqlmock.AnyArg(), "tn-alpha", "proj-one", "production").
		WillReturnResult(sqlmock.NewResult(0, 1))

	p := NewDataPlanePoller(prom, db, 30*time.Second, nil)
	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("Tick returned err: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations (skip logic wrong): %v", err)
	}
}

// TestDataPlanePoller_NeedLeaderElection — Given a constructed poller,
// When NeedLeaderElection is called, Then it returns true (EC-6:
// prevents concurrent scrapes when multiple operator replicas run).
func TestDataPlanePoller_NeedLeaderElection(t *testing.T) {
	p := NewDataPlanePoller(nil, nil, 30*time.Second, nil)
	if !p.NeedLeaderElection() {
		t.Fatalf("NeedLeaderElection = false; want true")
	}
}
