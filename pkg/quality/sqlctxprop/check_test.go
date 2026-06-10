// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package sqlctxprop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestActionableFix verifies the operator-facing message contains the
// critical fix shape `WithTx(tx)`.
func TestActionableFix(t *testing.T) {
	msg := ActionableFix()
	if !strings.Contains(msg, "WithTx(tx)") {
		t.Fatalf("ActionableFix() must mention WithTx(tx); got: %q", msg)
	}
	if !strings.Contains(msg, "withEnvironmentLock") {
		t.Fatalf("ActionableFix() must mention withEnvironmentLock; got: %q", msg)
	}
}

// TestCheckEmptyDirs ensures that when api/internal/routes and
// api/internal/services don't exist (or are empty), the gate trivially
// passes. Phase 0 baseline.
func TestCheckEmptyDirs(t *testing.T) {
	root := t.TempDir()
	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if !outcome.Pass {
		t.Fatalf("expected PASS on empty repo; got %d violations", len(outcome.Violations))
	}
}

// TestCheckPhase0Baseline simulates the Phase 0 baseline state: services/
// files exist but none import the sqlc-generated package. Result: PASS.
func TestCheckPhase0Baseline(t *testing.T) {
	root := t.TempDir()
	servicesDir := filepath.Join(root, defaultServicesDir)
	if err := os.MkdirAll(servicesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write a service file that does NOT import sqlc-generated.
	if err := os.WriteFile(filepath.Join(servicesDir, "foo.go"), []byte(`package services

func doWork(tx interface{}) error {
	return nil
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !outcome.Pass {
		t.Fatalf("expected Phase 0 baseline PASS; got %d violations: %+v", len(outcome.Violations), outcome.Violations)
	}
	if outcome.Scanned != 1 {
		t.Fatalf("expected 1 file scanned, got %d", outcome.Scanned)
	}
}

// TestCheckDetectsPoolBoundInsideLock simulates a service file that imports
// the sqlc-generated package AND calls a sqlc method inside a
// withEnvironmentLock closure WITHOUT WithTx threading. Result: FAIL.
func TestCheckDetectsPoolBoundInsideLock(t *testing.T) {
	root := t.TempDir()
	servicesDir := filepath.Join(root, defaultServicesDir)
	if err := os.MkdirAll(servicesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package services

import (
	"context"

	"github.com/loopzero/loopzero/api/internal/db/generated"
)

type Locker struct{}

func (l *Locker) withEnvironmentLock(ctx context.Context, tx interface{}, fn func(tx interface{}) error) error {
	return fn(tx)
}

type Service struct {
	locker  *Locker
	queries *generated.Queries
}

func (s *Service) Submit(ctx context.Context) error {
	return s.locker.withEnvironmentLock(ctx, nil, func(tx interface{}) error {
		// VIOLATION: pool-bound call inside lock closure
		_, _ = s.queries.GetReleaseByID(ctx, "id-1")
		return nil
	})
}
`
	if err := os.WriteFile(filepath.Join(servicesDir, "service.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if outcome.Pass {
		t.Fatalf("expected FAIL; got PASS with %d scanned", outcome.Scanned)
	}
	if len(outcome.Violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(outcome.Violations), outcome.Violations)
	}
	v := outcome.Violations[0]
	if !strings.Contains(v.Function, "GetReleaseByID") {
		t.Fatalf("expected violation on GetReleaseByID; got Function=%q", v.Function)
	}
	if !strings.Contains(v.Detail, "withEnvironmentLock") {
		t.Fatalf("expected Detail to mention withEnvironmentLock; got %q", v.Detail)
	}
}

// TestCheckAllowsWithTxThreading simulates the correct pattern: sqlc call
// INSIDE the lock closure uses .WithTx(tx).Method(...). Result: PASS.
func TestCheckAllowsWithTxThreading(t *testing.T) {
	root := t.TempDir()
	servicesDir := filepath.Join(root, defaultServicesDir)
	if err := os.MkdirAll(servicesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package services

import (
	"context"

	"github.com/loopzero/loopzero/api/internal/db/generated"
)

type Locker struct{}

func (l *Locker) withEnvironmentLock(ctx context.Context, tx interface{}, fn func(tx interface{}) error) error {
	return fn(tx)
}

type Service struct {
	locker  *Locker
	queries *generated.Queries
}

func (s *Service) Submit(ctx context.Context) error {
	return s.locker.withEnvironmentLock(ctx, nil, func(tx interface{}) error {
		// CORRECT: tx-bound via WithTx
		_, _ = s.queries.WithTx(tx).GetReleaseByID(ctx, "id-1")
		return nil
	})
}
`
	if err := os.WriteFile(filepath.Join(servicesDir, "service.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !outcome.Pass {
		t.Fatalf("expected PASS with WithTx threading; got %d violations: %+v", len(outcome.Violations), outcome.Violations)
	}
}

// TestCheckIgnoresPoolBoundOutsideLock ensures that calls OUTSIDE a
// withEnvironmentLock closure are NOT flagged (gate only enforces the
// tx-threading invariant inside lock blocks).
func TestCheckIgnoresPoolBoundOutsideLock(t *testing.T) {
	root := t.TempDir()
	servicesDir := filepath.Join(root, defaultServicesDir)
	if err := os.MkdirAll(servicesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package services

import (
	"context"

	"github.com/loopzero/loopzero/api/internal/db/generated"
)

type Service struct {
	queries *generated.Queries
}

// ReadOnly call — outside any lock; pool-bound is fine here.
func (s *Service) GetRelease(ctx context.Context, id string) error {
	_, err := s.queries.GetReleaseByID(ctx, id)
	return err
}
`
	if err := os.WriteFile(filepath.Join(servicesDir, "service.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !outcome.Pass {
		t.Fatalf("expected PASS for pool-bound OUTSIDE lock; got %d violations: %+v", len(outcome.Violations), outcome.Violations)
	}
}

// TestCheckIgnoresTestFiles ensures *_test.go files are not scanned (tests
// often use stubs and shouldn't trigger the gate).
func TestCheckIgnoresTestFiles(t *testing.T) {
	root := t.TempDir()
	servicesDir := filepath.Join(root, defaultServicesDir)
	if err := os.MkdirAll(servicesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package services

import (
	"context"

	"github.com/loopzero/loopzero/api/internal/db/generated"
)

type Locker struct{}

func (l *Locker) withEnvironmentLock(ctx context.Context, tx interface{}, fn func(tx interface{}) error) error {
	return fn(tx)
}

type Service struct {
	locker  *Locker
	queries *generated.Queries
}

func (s *Service) Submit(ctx context.Context) error {
	return s.locker.withEnvironmentLock(ctx, nil, func(tx interface{}) error {
		// Would be a violation in non-test code; but this is _test.go
		_, _ = s.queries.GetReleaseByID(ctx, "id-1")
		return nil
	})
}
`
	if err := os.WriteFile(filepath.Join(servicesDir, "service_test.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !outcome.Pass {
		t.Fatalf("expected PASS (test file ignored); got %d violations", len(outcome.Violations))
	}
}
