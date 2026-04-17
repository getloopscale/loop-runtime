// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

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

func TestEnqueueEviction_RejectsEmpty(t *testing.T) {
	ev := NewTenantEvictor(nil)
	_, err := ev.EnqueueEviction(context.Background(), nil)
	if err == nil {
		t.Fatalf("expected err on empty tenant list")
	}
}

func TestEnqueueEviction_ReturnsJobID(t *testing.T) {
	ev := NewTenantEvictor(nil)
	jobID, err := ev.EnqueueEviction(context.Background(), []string{"tn-a", "tn-b"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if jobID == "" {
		t.Fatalf("expected non-empty job id")
	}
}
