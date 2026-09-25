// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package secretsync

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/pkg/domain"
)

// TestCreateSecret_OnError_DoesNotLogValue is the T2.7 DoD test
// (find_sec_006 LOW of deep-review-remediation-plan).
//
// The secret manager has TWO failure paths; we exercise BOTH separately
// because each one logs differently:
//
//  1. mgmt cluster Secret create fails → fmt.Errorf("create k8s secret on
//     mgmt: %w", err) (wrapped error, no slog.Error from secret manager).
//  2. runtime cluster ExternalSecret CR create fails → mgmt path succeeds
//     first, then slog.Error("ExternalSecret create/update failed", ...)
//     fires in secret_k8s_manager.go:190.
//
// The earlier dogfood-2026-05-08-fixes T2.3 work removed the `value` field
// from the slog.Error call. This test is the regression guard: any future
// edit that re-adds the value as a slog field will fail the second
// sub-test (verified RED via /tmp/leak_inject.patch — adding
// `"INJECTED_LEAK_DEBUG", value` to the slog.Error makes the test fail).
func TestCreateSecret_OnError_DoesNotLogValue(t *testing.T) {
	const (
		secretValue = "super-sensitive-value-12345-NEVER-LEAK"
		tenantID    = "tn-x"
		projectID   = "p1"
		env         = "production"
		app         = "api"
		secretName  = "API_KEY"
	)

	t.Run("mgmt_create_fails_wrapped_error_no_value", func(t *testing.T) {
		var logBuf bytes.Buffer
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
		t.Cleanup(func() { slog.SetDefault(prev) })

		mgmtFake := fake.NewClientset()
		mgmtFake.PrependReactor("create", "secrets", func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("mgmt cluster Create failed")
		})

		scheme := runtime.NewScheme()
		dynFake := dynamicfake.NewSimpleDynamicClient(scheme)

		mgr := &SecretK8sManager{mgmtClient: mgmtFake, runtimeDynamic: dynFake, mgmtNamespace: "loopzero-system", tenantCells: fixedCell("cell-0"), runtimeCellID: "cell-0"}
		err := mgr.CreateSecret(context.Background(), ports.SecretRef{Env: domain.EnvRef{TenantID: tenantID, Project: projectID, EnvName: env}, App: app, Name: secretName}, secretValue)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "create k8s secret on mgmt") {
			t.Fatalf("expected mgmt-path wrapped error, got: %v", err)
		}
		if strings.Contains(err.Error(), secretValue) {
			t.Errorf("LEAK: secret value in wrapped error: %s", err.Error())
		}
		if strings.Contains(logBuf.String(), secretValue) {
			t.Errorf("LEAK: secret value in slog output: %s", logBuf.String())
		}
	})

	t.Run("runtime_externalsecret_create_fails_slog_no_value", func(t *testing.T) {
		var logBuf bytes.Buffer
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
		t.Cleanup(func() { slog.SetDefault(prev) })

		// mgmt succeeds (default fake); runtime dynamic fails on create.
		mgmtFake := fake.NewClientset()

		scheme := runtime.NewScheme()
		dynFake := dynamicfake.NewSimpleDynamicClient(scheme)
		dynFake.PrependReactor("create", "externalsecrets", func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("runtime ExternalSecret create failed")
		})

		mgr := &SecretK8sManager{mgmtClient: mgmtFake, runtimeDynamic: dynFake, mgmtNamespace: "loopzero-system", tenantCells: fixedCell("cell-0"), runtimeCellID: "cell-0"}
		err := mgr.CreateSecret(context.Background(), ports.SecretRef{Env: domain.EnvRef{TenantID: tenantID, Project: projectID, EnvName: env}, App: app, Name: secretName}, secretValue)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "create external secret on runtime") {
			t.Fatalf("expected runtime-path wrapped error, got: %v", err)
		}
		if strings.Contains(err.Error(), secretValue) {
			t.Errorf("LEAK: secret value in wrapped error: %s", err.Error())
		}
		// THE critical assertion: the slog.Error call at secret_k8s_manager.go:190
		// MUST NOT include the value field. RED-proven by injecting a value
		// field — this assertion catches it.
		if strings.Contains(logBuf.String(), secretValue) {
			t.Errorf("LEAK: secret value leaked into slog output\nvalue=%q\nlog=%s",
				secretValue, logBuf.String())
		}
		// Sanity: confirm we DID exercise the slog.Error path (otherwise
		// we'd be vacuously passing).
		if !strings.Contains(logBuf.String(), "ExternalSecret create/update failed") {
			t.Errorf("expected slog.Error to fire; log buf was: %s", logBuf.String())
		}
	})
}

// Sanity check that the import of corev1 is exercised — without this,
// some Go test runners complain about unused imports introduced for
// future test fixtures.
var _ = corev1.Secret{}
