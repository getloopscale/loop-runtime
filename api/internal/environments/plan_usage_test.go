// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package environments

import (
	"context"
	"errors"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

type fixedEnvironments struct {
	entries []ports.EnvironmentEntry
	err     error
}

func (f fixedEnvironments) ListEnvironments(context.Context, string) ([]ports.EnvironmentEntry, error) {
	return f.entries, f.err
}

func TestPermanentEnvironments_CountsAnEnvironmentThatWasNeverDeployed(t *testing.T) {
	usage := PlanUsageFromEnvironments(fixedEnvironments{entries: []ports.EnvironmentEntry{
		{Name: "production"},                    // the legacy entry with no project
		{Name: "production", Project: "web"},    // deployed
		{Name: "preview", Project: "web"},       // the preview slot
		{Name: "preview-pr-7", Project: "web"},  // a pull request preview
		{Name: "qa", Project: "web"},            // created, nothing deployed to it
		{Name: "production", Project: "worker"}, // another project
	}})

	got, err := usage.PermanentEnvironments(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("PermanentEnvironments: %v", err)
	}
	if got != 3 {
		t.Errorf("permanent environments = %d, want 3 (web/production, web/qa, worker/production)", got)
	}
}

func TestPermanentEnvironments_ReturnsTheListError(t *testing.T) {
	boom := errors.New("tenant CR unreadable")
	usage := PlanUsageFromEnvironments(fixedEnvironments{err: boom})

	if _, err := usage.PermanentEnvironments(context.Background(), "tenant-1"); !errors.Is(err, boom) {
		t.Errorf("error = %v, want the list error so the gate fails closed", err)
	}
}
