// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package release

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

// fakeLabelReader is an in-memory ArgoAppLabelReader for tests.
type fakeLabelReader struct {
	labels map[string]map[string]string // appName → labels
	err    error                        // optional injected error
	calls  int
}

func (f *fakeLabelReader) GetAppLabels(_ context.Context, appName string) (map[string]string, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.labels[appName], nil
}

func newActivatorForLookupTest() *SyncCallbackActivator {
	return &SyncCallbackActivator{logger: slog.Default()}
}

// labelsFor returns a reader that answers the given Application with its
// tenant and environment labels.
func labelsFor(appName, tenant, env string) *fakeLabelReader {
	return &fakeLabelReader{labels: map[string]map[string]string{
		appName: {"runtime.getloopscale.com/tenant": tenant, "runtime.getloopscale.com/environment": env},
	}}
}

// The labels are authoritative even where every segment of the name contains a
// hyphen — the case a positional parse of the name gets wrong.
func TestLookupTenantEnv_ReadsTheIdentityFromTheLabels(t *testing.T) {
	const appName = "tn-loopruntime-web-j-rollout-a"
	reader := labelsFor(appName, "tn-loopruntime", "j-rollout-a")
	a := newActivatorForLookupTest().WithAppLabelReader(reader)

	tenant, env, err := a.lookupTenantEnv(context.Background(), appName)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tenant != "tn-loopruntime" || env != "j-rollout-a" {
		t.Errorf("got (%q, %q), want (tn-loopruntime, j-rollout-a)", tenant, env)
	}
	if reader.calls != 1 {
		t.Errorf("expected exactly 1 reader call, got %d", reader.calls)
	}
}

func TestLookupTenantEnv_FailsWhenTheApplicationHasNoLabels(t *testing.T) {
	const appName = "tn-acme-web-production"
	a := newActivatorForLookupTest().WithAppLabelReader(&fakeLabelReader{labels: map[string]map[string]string{appName: nil}})

	_, _, err := a.lookupTenantEnv(context.Background(), appName)

	if !errors.Is(err, errAppIdentityUnresolved) {
		t.Fatalf("want errAppIdentityUnresolved, got %v", err)
	}
}

// A partial label set must not yield a (tenant, "") pair that silently matches
// no release.
func TestLookupTenantEnv_FailsWhenTheEnvironmentLabelIsMissing(t *testing.T) {
	const appName = "tn-acme-web-production"
	reader := &fakeLabelReader{labels: map[string]map[string]string{appName: {"runtime.getloopscale.com/tenant": "tn-acme"}}}
	a := newActivatorForLookupTest().WithAppLabelReader(reader)

	_, _, err := a.lookupTenantEnv(context.Background(), appName)

	if !errors.Is(err, errAppIdentityUnresolved) {
		t.Fatalf("want errAppIdentityUnresolved, got %v", err)
	}
}

func TestLookupTenantEnv_FailsWhenTheReaderErrors(t *testing.T) {
	a := newActivatorForLookupTest().WithAppLabelReader(&fakeLabelReader{err: errors.New("k8s API unreachable")})

	_, _, err := a.lookupTenantEnv(context.Background(), "tn-acme-web-production")

	if !errors.Is(err, errAppIdentityUnresolved) {
		t.Fatalf("want errAppIdentityUnresolved, got %v", err)
	}
}

func TestLookupTenantEnv_FailsWhenNoReaderIsWired(t *testing.T) {
	_, _, err := newActivatorForLookupTest().lookupTenantEnv(context.Background(), "tn-acme-web-production")

	if !errors.Is(err, errAppIdentityUnresolved) {
		t.Fatalf("want errAppIdentityUnresolved, got %v", err)
	}
}
