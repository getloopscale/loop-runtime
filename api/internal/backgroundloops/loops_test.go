// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package backgroundloops

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
)

type stubCellLister struct{}

func (stubCellLister) ListCellIDs(context.Context) ([]string, error) { return nil, nil }

func loopNames(loops []Loop) []string {
	names := make([]string, 0, len(loops))
	for _, l := range loops {
		names = append(names, l.Name)
	}
	return names
}

func buildWith(t *testing.T, d Deps) []Loop {
	t.Helper()
	d.WorkerPool = &db.PgxAdapter{}
	return Build(config.Config{BaseDomain: "example.test"}, d, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestBuildReturnsEveryLoopEitherBinaryRuns(t *testing.T) {
	// Arrange / Act
	names := loopNames(buildWith(t, Deps{}))

	// Assert — the loops the controller's hand-wired copy used to lack are present.
	want := []string{
		"build-reconcile", "build-reaper", "release-reaper", "release-convergence",
		"idempotency-cleaner", "event-pruner", "cell-metrics", "build-gc",
		"audit-log-pruner", "domain-provisioner", "domain-health-checker",
	}
	if !slices.Equal(names, want) {
		t.Fatalf("loops = %v, want %v", names, want)
	}
}

func TestBuildAddsOrphanDetectorOnlyWithACellLister(t *testing.T) {
	// Arrange
	var lister deploycontracts.CellLister = stubCellLister{}

	// Act
	names := loopNames(buildWith(t, Deps{CellLister: lister}))

	// Assert
	if !slices.Contains(names, "orphan-detector") {
		t.Fatalf("loops = %v, want orphan-detector when a CellLister is wired", names)
	}
}

func TestCNAMETargetIsTheCnameLabelUnderTheBaseDomain(t *testing.T) {
	if got := CNAMETarget(config.Config{BaseDomain: "apps.example.test"}); got != "cname.apps.example.test" {
		t.Fatalf("CNAMETarget = %q", got)
	}
}
