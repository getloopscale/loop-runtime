// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// B-177 — one function, four exits, two of them counting.
//
// CreateSecret is called after routes/secrets has already persisted the row
// via store.Set. Every way out of it therefore happens with a row already in
// `secrets`. Two of those ways out moved
// loopruntime_secret_externalsecret_total: the ExternalSecret create/update failure
// ("fail") and the success ("success"). The other two returned before either
// counter — the mgmt-cluster Secret create/update failure, and the
// namespace-derivation failure — so a row could land in `secrets` with
// sum(loopruntime_secret_externalsecret_total) still at zero.
//
// That sum is the trigger the B-126 canary deferral
// (docs/wiki/decisions/2026-09-04-secret-decrypt-canary-deferral.md § 2) names
// as the one that fires "without anyone looking". A hole in it is a deferral
// that never becomes work.
//
// This is NOT B-176's finding. B-176 was asymmetry BETWEEN two routes — the
// bulk path and the single-set path handling `k8sSync == nil` differently.
// This is incomplete coverage INSIDE one function: every early return of
// CreateSecret, not the choice of caller.
//
// The test asserts all four exits in one place, on purpose. Coverage that is
// complete today decays one early return at a time, and a test that watches
// only the exits that already counted would not notice the fifth being added.

package secretsync

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/getloopscale/loop-runtime/api/internal/metrics"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

// externalSecretResult reads one series. Used as a DELTA: the counter is a
// package-level global shared with every other test in this binary, so an
// absolute value would depend on test execution order.
func externalSecretResult(result string) float64 {
	return testutil.ToFloat64(metrics.SecretExternalSecretTotal.WithLabelValues(result))
}

// TestCreateSecret_EveryExitMovesTheCounter locks the coverage. Deleting the
// increment behind any one exit turns the B-126 deferral trigger back into a
// signal that can miss the event it exists to announce.
func TestCreateSecret_EveryExitMovesTheCounter(t *testing.T) {
	const (
		tenantID   = "tn-b177"
		projectID  = "p1"
		env        = "production"
		app        = "api"
		secretKey  = "API_KEY"
		secretVal  = "value-never-asserted-on"
		mgmtNS     = "loopruntime-system"
		wantResult = "fail"
	)

	// newManager wires a manager whose mgmt client and dynamic client are both
	// healthy fakes. Each sub-test breaks exactly the one thing it is about.
	newManager := func(mgmt *fake.Clientset, dyn *dynamicfake.FakeDynamicClient) *SecretK8sManager {
		return &SecretK8sManager{mgmtClient: mgmt, runtimeDynamic: dyn, mgmtNamespace: mgmtNS, tenantCells: fixedCell("cell-0"), runtimeCellID: "cell-0"}
	}
	newDyn := func() *dynamicfake.FakeDynamicClient {
		return newRuntimeFake()
	}

	t.Run("mgmt secret write fails", func(t *testing.T) {
		mgmtFake := fake.NewClientset()
		mgmtFake.PrependReactor("create", "secrets", func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("mgmt cluster Create failed")
		})

		before := externalSecretResult(wantResult)
		err := newManager(mgmtFake, newDyn()).
			CreateSecret(context.Background(), ports.SecretRef{TenantID: tenantID, Project: projectID, Environment: env, App: app, Name: secretKey}, secretVal)
		if err == nil {
			t.Fatal("expected the mgmt-cluster Secret write to fail; the sub-test asserts nothing otherwise")
		}
		got := externalSecretResult(wantResult) - before

		if got != 1 {
			t.Errorf("mgmt-write failure moved loopruntime_secret_externalsecret_total{result=%q} by %v, want exactly 1 — B-177: store.Set has already persisted the row when CreateSecret is reached, so an exit that counts nothing leaves a row in `secrets` with sum(loopruntime_secret_externalsecret_total) at zero, and the B-126 trigger misses it (err was: %v)",
				wantResult, got, err)
		}
	})

	t.Run("tenant placed on a cell this instance does not serve", func(t *testing.T) {
		mgr := newManager(fake.NewClientset(), newDyn())
		mgr.tenantCells = fixedCell("cell-1")

		before := externalSecretResult(wantResult)
		err := mgr.CreateSecret(context.Background(), ports.SecretRef{TenantID: tenantID, Project: projectID, Environment: env, App: app, Name: secretKey}, secretVal)
		if err == nil {
			t.Fatal("expected the placement check to refuse; the sub-test asserts nothing otherwise")
		}
		if got := externalSecretResult(wantResult) - before; got != 1 {
			t.Errorf("placement refusal moved loopruntime_secret_externalsecret_total{result=%q} by %v, want exactly 1 — the row is already persisted by the time CreateSecret runs (err was: %v)", wantResult, got, err)
		}
	})

	t.Run("namespace derivation fails", func(t *testing.T) {
		// The mgmt write SUCCEEDS here — the default fake accepts it — so this
		// exercises the second uncounted return specifically. An empty env
		// makes domain.NewEnvRef reject the ref; the mgmt Secret name does not
		// contain env, so the write above it still lands.
		mgmtFake := fake.NewClientset()

		before := externalSecretResult(wantResult)
		err := newManager(mgmtFake, newDyn()).
			CreateSecret(context.Background(), ports.SecretRef{TenantID: tenantID, Project: projectID, Environment: "", App: app, Name: secretKey}, secretVal)
		if err == nil {
			t.Fatal("expected namespace derivation to fail on an empty env; the sub-test asserts nothing otherwise")
		}
		got := externalSecretResult(wantResult) - before

		if got != 1 {
			t.Errorf("namespace-derivation failure moved loopruntime_secret_externalsecret_total{result=%q} by %v, want exactly 1 — B-177: same reason as the sub-test above; the row is already persisted by the time this return is taken (err was: %v)",
				wantResult, got, err)
		}
	})

	// The two exits that already counted, asserted here so the file is the
	// whole contract of the function rather than only its repaired half. If a
	// later edit "simplifies" by folding the new increments into a place that
	// double-counts these, these two catch it.
	t.Run("externalsecret write fails", func(t *testing.T) {
		mgmtFake := fake.NewClientset()
		dynFake := newDyn()
		dynFake.PrependReactor("create", "externalsecrets", func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("runtime ExternalSecret create failed")
		})

		before := externalSecretResult("fail")
		err := newManager(mgmtFake, dynFake).
			CreateSecret(context.Background(), ports.SecretRef{TenantID: tenantID, Project: projectID, Environment: env, App: app, Name: secretKey}, secretVal)
		if err == nil {
			t.Fatal("expected the ExternalSecret write to fail; the sub-test asserts nothing otherwise")
		}
		if got := externalSecretResult("fail") - before; got != 1 {
			t.Errorf("ExternalSecret-write failure moved loopruntime_secret_externalsecret_total{result=\"fail\"} by %v, want exactly 1", got)
		}
	})

	t.Run("success", func(t *testing.T) {
		mgmtFake := fake.NewClientset()

		beforeOK := externalSecretResult("success")
		beforeFail := externalSecretResult("fail")
		if err := newManager(mgmtFake, newDyn()).
			CreateSecret(context.Background(), ports.SecretRef{TenantID: tenantID, Project: projectID, Environment: env, App: app, Name: secretKey}, secretVal); err != nil {
			t.Fatalf("expected the happy path to succeed against healthy fakes, got: %v", err)
		}
		if got := externalSecretResult("success") - beforeOK; got != 1 {
			t.Errorf("success moved loopruntime_secret_externalsecret_total{result=\"success\"} by %v, want exactly 1", got)
		}
		if got := externalSecretResult("fail") - beforeFail; got != 0 {
			t.Errorf("success also moved loopruntime_secret_externalsecret_total{result=\"fail\"} by %v, want 0 — a success must not be counted as a failure too", got)
		}
	})
}
