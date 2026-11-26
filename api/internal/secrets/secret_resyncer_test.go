// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package secrets

// secret_resyncer_test.go — T7.2 of plan 2026-06-01-p3-infra-observability-
// test-debt-plan.md v1.1 — regression tests for V9 L20 fix
// (secret resync on first deploy).
//
// L20 bug recap: `loopzero secret set --env <new>` BEFORE the first deploy
// silently failed the K8s sync at POST /api/v1/secrets time because the
// workload namespace did not yet exist. Pods later failed to start with
// CreateContainerConfigError because the K8s Secret was never created on
// the workload cluster.
//
// Fix shipped in routes/builds.go:330-339 — BuildHandler calls
// h.secretResync.ResyncEnv(...) after envEnsurer.EnsureEnvironment so all
// PG-stored secrets are (re)applied to the workload cluster as soon as
// the namespace becomes available. SecretResyncer.ResyncEnv itself lives
// in this package (secret_resyncer.go).
//
// These tests prove BOTH directions:
//   1. ResyncEnv with non-empty PG store → K8s.CreateSecret invoked once
//      per entry with the exact (tenant, project, env, app, name, value)
//      tuple. (TestSecretResyncer_L20_FirstDeploy)
//   2. nil resyncer is a safe no-op — proves the wiring guard works
//      (TestSecretResyncer_L20_NilResyncer_NotSynced).
//   3. Per-secret K8s failures are logged but do not abort the resync
//      (TestSecretResyncer_L20_PerSecretError_DoesNotAbort).
//
// Pure unit tests — no PG, no K8s — both interfaces are mocked.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

// fakeSecretResyncStore implements SecretResyncStore with a configurable
// fixed entry list (or error).
type fakeSecretResyncStore struct {
	entries []store.SecretResyncEntry
	err     error
	calls   int
	lastT   string
	lastP   string
	lastE   string
}

func (f *fakeSecretResyncStore) ListForResync(_ context.Context, tenantID, projectID, env string) ([]store.SecretResyncEntry, error) {
	f.calls++
	f.lastT, f.lastP, f.lastE = tenantID, projectID, env
	if f.err != nil {
		return nil, f.err
	}
	return f.entries, nil
}

// secretCall captures a K8s.CreateSecret invocation for assertions.
type secretCall struct {
	TenantID  string
	ProjectID string
	Env       string
	App       string
	Name      string
	Value     string
}

// fakeSecretK8sCreator implements SecretK8sCreator and records every
// CreateSecret call. A non-nil errFor func returns an error for matching
// calls so per-secret failure paths can be tested.
type fakeSecretK8sCreator struct {
	mu     sync.Mutex
	calls  []secretCall
	errFor func(c secretCall) error
}

func (f *fakeSecretK8sCreator) CreateSecret(_ context.Context, ref ports.SecretRef, value string) error {
	c := secretCall{
		TenantID:  ref.TenantID,
		ProjectID: ref.Project,
		Env:       ref.Environment,
		App:       ref.App,
		Name:      ref.Name,
		Value:     value,
	}
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()
	if f.errFor != nil {
		return f.errFor(c)
	}
	return nil
}

func (f *fakeSecretK8sCreator) callsCopy() []secretCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]secretCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// TestSecretResyncer_L20_FirstDeploy (T7.2 RED #1): secret pre-set in
// PG before the first deploy → ResyncEnv invokes K8s.CreateSecret with
// the same tuple. This proves the pipeline reaches the cell-0-cluster
// sync that the original POST /secrets call missed.
func TestSecretResyncer_L20_FirstDeploy(t *testing.T) {
	store := &fakeSecretResyncStore{
		entries: []store.SecretResyncEntry{
			{Name: "db-password", App: "api", Value: "supersecret"},
		},
	}
	k8s := &fakeSecretK8sCreator{}
	r := &SecretResyncer{Store: store, K8s: k8s}

	err := r.ResyncEnv(context.Background(), "tn-a", "proj-x", "production")
	if err != nil {
		t.Fatalf("ResyncEnv: %v", err)
	}

	if store.calls != 1 {
		t.Errorf("expected exactly 1 ListForResync call, got %d", store.calls)
	}
	if store.lastT != "tn-a" || store.lastP != "proj-x" || store.lastE != "production" {
		t.Errorf("ListForResync scope drift: got (%q,%q,%q)", store.lastT, store.lastP, store.lastE)
	}

	calls := k8s.callsCopy()
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 CreateSecret call, got %d: %+v", len(calls), calls)
	}
	want := secretCall{
		TenantID:  "tn-a",
		ProjectID: "proj-x",
		Env:       "production",
		App:       "api",
		Name:      "db-password",
		Value:     "supersecret",
	}
	if calls[0] != want {
		t.Errorf("CreateSecret tuple mismatch:\n  got:  %+v\n  want: %+v", calls[0], want)
	}
}

// TestSecretResyncer_L20_NilResyncer_NotSynced (T7.2 RED #2): proves the
// "nil-handler" guard in ResyncEnv returns nil without panicking AND that
// the test catches the regression — if a future refactor accidentally
// dereferences Store/K8s before the guard, this test panics (the guard
// must be the FIRST thing in ResyncEnv).
//
// Mirrors the "nil guard" pattern of routes/builds.go:330 (`if
// h.secretResync != nil`). Without this test, a wiring drop that swaps
// the wired resyncer for nil would silently re-introduce L20 — pods
// would fail to start at first deploy with no operator-visible signal.
func TestSecretResyncer_L20_MisconfiguredResyncerFailsLoud(t *testing.T) {
	// A wired resyncer missing a dependency is a wiring bug: reporting success
	// would silently re-introduce L20 (pods start before their secrets exist).
	var nilR *SecretResyncer
	if err := nilR.ResyncEnv(context.Background(), "tn-a", "proj-x", "production"); !errors.Is(err, ErrSecretResyncerMisconfigured) {
		t.Errorf("nil receiver: want ErrSecretResyncerMisconfigured, got %v", err)
	}

	rStore := &SecretResyncer{Store: nil, K8s: &fakeSecretK8sCreator{}}
	if err := rStore.ResyncEnv(context.Background(), "tn-a", "proj-x", "production"); !errors.Is(err, ErrSecretResyncerMisconfigured) {
		t.Errorf("nil Store: want ErrSecretResyncerMisconfigured, got %v", err)
	}

	// Nil K8s must refuse before listing: secrets it cannot apply are not read.
	store := &fakeSecretResyncStore{}
	rK8s := &SecretResyncer{Store: store, K8s: nil}
	if err := rK8s.ResyncEnv(context.Background(), "tn-a", "proj-x", "production"); !errors.Is(err, ErrSecretResyncerMisconfigured) {
		t.Errorf("nil K8s: want ErrSecretResyncerMisconfigured, got %v", err)
	}
	if store.calls != 0 {
		t.Errorf("nil K8s short-circuit MUST NOT call ListForResync; got %d calls", store.calls)
	}
}

// TestSecretResyncer_L20_PerSecretError_DoesNotAbort (T7.2 RED #3):
// per-secret failures from K8s.CreateSecret are propagated as a wrapped
// error but ALL secrets are attempted (the first failure doesn't abort
// the loop). Important because in production a single transient
// ExternalSecret webhook timeout shouldn't block resync of a 10-secret
// namespace — each pod-start failure has its own surfacing path.
func TestSecretResyncer_L20_PerSecretError_DoesNotAbort(t *testing.T) {
	store := &fakeSecretResyncStore{
		entries: []store.SecretResyncEntry{
			{Name: "ok-1", App: "api", Value: "v1"},
			{Name: "fail-2", App: "api", Value: "v2"},
			{Name: "ok-3", App: "worker", Value: "v3"},
		},
	}
	k8s := &fakeSecretK8sCreator{
		errFor: func(c secretCall) error {
			if c.Name == "fail-2" {
				return errors.New("k8s: ExternalSecret webhook timeout")
			}
			return nil
		},
	}
	r := &SecretResyncer{Store: store, K8s: k8s}

	err := r.ResyncEnv(context.Background(), "tn-x", "proj-y", "staging")
	if err == nil {
		t.Fatal("expected wrapped error when one secret fails, got nil")
	}

	calls := k8s.callsCopy()
	if len(calls) != 3 {
		t.Errorf("ALL 3 secrets must be attempted (don't abort on first fail); got %d calls", len(calls))
	}
	// Verify the failed secret was attempted at its scheduled position
	// (loop didn't early-return).
	names := []string{}
	for _, c := range calls {
		names = append(names, c.Name)
	}
	wantNames := []string{"ok-1", "fail-2", "ok-3"}
	for i, n := range wantNames {
		if i >= len(names) || names[i] != n {
			t.Errorf("call %d: got %q, want %q (loop ordering drift)", i,
				safeIndex(names, i), n)
		}
	}
}

func safeIndex(s []string, i int) string {
	if i < 0 || i >= len(s) {
		return "<MISSING>"
	}
	return s[i]
}

// TestSecretResyncer_NamespaceTerminating_Retries (LIVE-caught dogfood
// trio 2026-06-02 Journey 03 Secrets Lifecycle): when K8s.CreateSecret
// returns "is being terminated" on the first attempt, the resyncer
// MUST retry with bounded backoff until namespace is recreated by
// envEnsurer-triggered Tenant CR reconcile. Genuine errors fail fast.
func TestSecretResyncer_NamespaceTerminating_Retries(t *testing.T) {
	// K8s mock that returns terminating error on first call, success on second.
	callsByName := make(map[string]int)
	var mu sync.Mutex
	k8s := &fakeSecretK8sCreator{
		errFor: func(c secretCall) error {
			mu.Lock()
			defer mu.Unlock()
			callsByName[c.Name]++
			if callsByName[c.Name] == 1 {
				return fmt.Errorf("create external secret on runtime: %w", namespaceTerminatingErr("loopzero-e2e-tenant-j-secrets"))
			}
			return nil // succeed on retry
		},
	}
	store := &fakeSecretResyncStore{
		entries: []store.SecretResyncEntry{
			{Name: "SECRET_PING", App: "api", Value: "v1"},
		},
	}
	r := &SecretResyncer{Store: store, K8s: k8s}

	err := r.ResyncEnv(context.Background(), "tn-a", "proj-x", "j-secrets")
	if err != nil {
		t.Fatalf("ResyncEnv MUST succeed after retry, got: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if callsByName["SECRET_PING"] != 2 {
		t.Errorf("expected exactly 2 attempts (1 terminating + 1 success), got %d", callsByName["SECRET_PING"])
	}
}

// TestSecretResyncer_NamespaceTerminating_TimeoutFailsExplicit: if the
// namespace stays terminating beyond the retry window, the resyncer
// MUST return a wrapped error (not infinite hang).
func TestSecretResyncer_NamespaceTerminating_TimeoutFailsExplicit(t *testing.T) {
	// K8s mock that ALWAYS returns terminating error.
	k8s := &fakeSecretK8sCreator{
		errFor: func(c secretCall) error {
			return namespaceTerminatingErr("loopzero-e2e-tenant-j-secrets")
		},
	}
	store := &fakeSecretResyncStore{
		entries: []store.SecretResyncEntry{
			{Name: "SECRET", App: "api", Value: "v"},
		},
	}
	r := &SecretResyncer{Store: store, K8s: k8s}

	// Use bounded ctx to avoid waiting the full 60s window in CI; the
	// retry loop's select honors ctx.Done.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := r.ResyncEnv(ctx, "tn-a", "proj-x", "j-secrets")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error when namespace stays terminating, got nil")
	}
	// Must return within ~ctx timeout, not the full 60s.
	if elapsed > 2*time.Second {
		t.Errorf("expected fast return on ctx done; took %v", elapsed)
	}
}

// TestSecretResyncer_NonTerminatingError_FailsFast: errors that are
// NOT "is being terminated" (e.g., RBAC, malformed) MUST fail
// immediately without retry — only the specific terminating-NS
// error class triggers retry.
func TestSecretResyncer_NonTerminatingError_FailsFast(t *testing.T) {
	callCount := 0
	k8s := &fakeSecretK8sCreator{
		errFor: func(c secretCall) error {
			callCount++
			return errors.New(`forbidden: user "system:serviceaccount:..." cannot create externalsecrets`)
		},
	}
	store := &fakeSecretResyncStore{
		entries: []store.SecretResyncEntry{
			{Name: "SECRET", App: "api", Value: "v"},
		},
	}
	r := &SecretResyncer{Store: store, K8s: k8s}

	start := time.Now()
	err := r.ResyncEnv(context.Background(), "tn-a", "proj-x", "j-secrets")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error for non-terminating failure, got nil")
	}
	if callCount != 1 {
		t.Errorf("expected exactly 1 attempt (no retry for non-terminating), got %d", callCount)
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("non-terminating error MUST fail fast; took %v", elapsed)
	}
}

func TestSecretResyncer_ReportsHowManySecretsFailed(t *testing.T) {
	rs := &fakeSecretResyncStore{entries: []store.SecretResyncEntry{{App: "api", Name: "A"}, {App: "api", Name: "B"}, {App: "api", Name: "C"}}}
	k8s := &fakeSecretK8sCreator{errFor: func(c secretCall) error {
		if c.Name == "C" {
			return nil
		}
		return errors.New("forbidden")
	}}
	r := &SecretResyncer{Store: rs, K8s: k8s}

	err := r.ResyncEnv(context.Background(), "tn-a", "proj-x", "production")

	if err == nil || !strings.Contains(err.Error(), "2/3 secrets failed") {
		t.Errorf("want the real failure count 2/3, got %v", err)
	}
}

// namespaceTerminatingErr builds the error the NamespaceLifecycle admission
// plugin returns: a Forbidden status carrying the NamespaceTerminating cause.
func namespaceTerminatingErr(ns string) error {
	err := apierrors.NewForbidden(
		schema.GroupResource{Group: "external-secrets.io", Resource: "externalsecrets"}, "es-api-secret-ping",
		fmt.Errorf("unable to create new content in namespace %s because it is being terminated", ns))
	err.ErrStatus.Details.Causes = append(err.ErrStatus.Details.Causes, metav1.StatusCause{
		Type:    corev1.NamespaceTerminatingCause,
		Message: fmt.Sprintf("namespace %s is being terminated", ns),
		Field:   "metadata.namespace",
	})
	return err
}

// A plain error whose text happens to say "is being terminated" is not the
// admission rejection: only the typed NamespaceTerminating cause may trigger
// the retry window. Matching the text retried unrelated failures for 60s.
func TestSecretResyncer_TerminatingTextWithoutCause_FailsFast(t *testing.T) {
	calls := 0
	k8s := &fakeSecretK8sCreator{errFor: func(secretCall) error {
		calls++
		return errors.New("proxy: upstream connection is being terminated")
	}}
	r := &SecretResyncer{
		Store: &fakeSecretResyncStore{entries: []store.SecretResyncEntry{{Name: "SECRET", App: "api", Value: "v"}}},
		K8s:   k8s,
	}

	err := r.ResyncEnv(context.Background(), "tn-a", "proj-x", "j-secrets")

	if err == nil {
		t.Fatal("expected the untyped error to fail the resync, got nil")
	}
	if calls != 1 {
		t.Errorf("expected exactly 1 attempt, got %d", calls)
	}
}
