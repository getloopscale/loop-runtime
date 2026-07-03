// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

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

// TestLookupTenantEnv_LabelsPresent_BeatsParser verifies the label-based path
// closes the code-005 hyphen ambiguity. App name `tn-loopzero-j-rollout-a`
// would be LastIndex-parsed as (tn-loopzero-j-rollout, a) which is WRONG —
// the correct (tenant, env) per labels is (tn-loopzero, j-rollout-a). The
// activator must prefer the labels.
func TestLookupTenantEnv_LabelsPresent_BeatsParser(t *testing.T) {
	const appName = "tn-loopzero-j-rollout-a"
	reader := &fakeLabelReader{
		labels: map[string]map[string]string{
			appName: {
				"loopzero.io/tenant":      "tn-loopzero",
				"loopzero.io/environment": "j-rollout-a",
			},
		},
	}
	a := newActivatorForLookupTest().WithAppLabelReader(reader)

	tenant, env, ok := a.lookupTenantEnv(context.Background(), appName)
	if !ok {
		t.Fatal("expected ok=true via label path")
	}
	if tenant != "tn-loopzero" {
		t.Errorf("tenant = %q, want tn-loopzero (parser would have given tn-loopzero-j-rollout — WRONG)", tenant)
	}
	if env != "j-rollout-a" {
		t.Errorf("env = %q, want j-rollout-a (parser would have given 'a' — WRONG)", env)
	}
	if reader.calls != 1 {
		t.Errorf("expected exactly 1 reader call, got %d", reader.calls)
	}
}

// TestLookupTenantEnv_NoLabels_FallsBackToParser confirms backward compat:
// when an App has no labels (legacy app pre-dating commit 8854f64e), the
// activator falls back to ParseArgoAppName so existing fixtures keep
// working unchanged.
func TestLookupTenantEnv_NoLabels_FallsBackToParser(t *testing.T) {
	const appName = "tn-acme-production"
	reader := &fakeLabelReader{
		labels: map[string]map[string]string{
			appName: nil, // App exists, but no labels set
		},
	}
	a := newActivatorForLookupTest().WithAppLabelReader(reader)

	tenant, env, ok := a.lookupTenantEnv(context.Background(), appName)
	if !ok {
		t.Fatal("expected ok=true via parser fallback")
	}
	if tenant != "tn-acme" {
		t.Errorf("tenant = %q, want tn-acme (parser path)", tenant)
	}
	if env != "production" {
		t.Errorf("env = %q, want production (parser path)", env)
	}
}

// TestLookupTenantEnv_OnlyTenantLabel_FallsBackToParser confirms that
// partial labels (tenant present, env missing) trigger the fallback —
// rather than returning a bogus (tenant, "") pair that would break
// downstream FindPendingReleases lookup.
func TestLookupTenantEnv_OnlyTenantLabel_FallsBackToParser(t *testing.T) {
	const appName = "tn-acme-production"
	reader := &fakeLabelReader{
		labels: map[string]map[string]string{
			appName: {
				"loopzero.io/tenant": "tn-acme",
				// loopzero.io/environment INTENTIONALLY missing
			},
		},
	}
	a := newActivatorForLookupTest().WithAppLabelReader(reader)

	tenant, env, ok := a.lookupTenantEnv(context.Background(), appName)
	if !ok {
		t.Fatal("expected fallback to parser when env label missing")
	}
	if tenant != "tn-acme" || env != "production" {
		t.Errorf("got (%q, %q), want (tn-acme, production) via parser fallback", tenant, env)
	}
}

// TestLookupTenantEnv_ReaderError_FallsBackToParser confirms that a K8s API
// error (NotFound, timeout, etc.) doesn't break callback activation — the
// activator silently falls back to the LastIndex parser.
func TestLookupTenantEnv_ReaderError_FallsBackToParser(t *testing.T) {
	const appName = "tn-acme-production"
	reader := &fakeLabelReader{err: errors.New("k8s API unreachable")}
	a := newActivatorForLookupTest().WithAppLabelReader(reader)

	tenant, env, ok := a.lookupTenantEnv(context.Background(), appName)
	if !ok {
		t.Fatal("expected fallback to parser when reader errors")
	}
	if tenant != "tn-acme" || env != "production" {
		t.Errorf("got (%q, %q), want (tn-acme, production) via parser fallback", tenant, env)
	}
}

// TestLookupTenantEnv_NoReaderWired_FallsBackToParser confirms zero-config
// behavior: when WithAppLabelReader is never called (e.g. in tests), the
// activator behaves exactly as the pre-Sprint-B-B3 version did.
func TestLookupTenantEnv_NoReaderWired_FallsBackToParser(t *testing.T) {
	a := newActivatorForLookupTest() // intentionally no WithAppLabelReader

	tenant, env, ok := a.lookupTenantEnv(context.Background(), "tn-acme-production")
	if !ok || tenant != "tn-acme" || env != "production" {
		t.Errorf("got ok=%v (%q, %q), want ok=true (tn-acme, production)", ok, tenant, env)
	}
}

// TestLookupTenantEnv_InvalidAppName_BothPathsFail confirms the final
// error path when both label lookup AND parser fail. This is the only
// case where OnSyncCompleted returns "invalid app name".
func TestLookupTenantEnv_InvalidAppName_BothPathsFail(t *testing.T) {
	a := newActivatorForLookupTest()
	_, _, ok := a.lookupTenantEnv(context.Background(), "nodelimiter")
	if ok {
		t.Fatal("expected ok=false for app name without hyphen")
	}
}
