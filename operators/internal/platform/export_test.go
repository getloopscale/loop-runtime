// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Test fakes for caFetcher injection in envtest specs.
// Plan 2026-05-25-live-ca-fetch-clustercredential T3.1 — used by
// op_b_envtest_test.go new specs that exercise the live-fetch reconcile path.
//
// export_test.go is the Go idiom for this: it is `package platform`, so it
// can reach unexported identifiers, and its _test.go suffix keeps it out of
// the operator binary while external specs in `package platform_test` still
// see the exported wrappers below.

package platform

import (
	"context"
	"sync/atomic"

	"k8s.io/client-go/rest"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
)

// NewReconcilerForTesting constructs a standalone ClusterCredentialReconciler
// outside the manager wiring. Used by envtest specs that need to inject
// custom fakes (StubCAFetcher / AlwaysFailFetcher) without rebuilding the
// whole manager.
func NewReconcilerForTesting(client ctrlclient.Client, argoCDNamespace string, fetcher caFetcher) *ClusterCredentialReconciler {
	return &ClusterCredentialReconciler{
		Client:          client,
		ArgoCDNamespace: argoCDNamespace,
		CAFetcher:       fetcher,
	}
}

// RunReconcileArgoCDSecretForTesting exposes the package-private
// reconcileArgoCDClusterSecret for envtest specs in package platform_test.
// Allows specs to drive the secret-render path directly with custom CRs +
// kubeconfigs without needing a full Reconcile() pass.
func RunReconcileArgoCDSecretForTesting(
	ctx context.Context,
	r *ClusterCredentialReconciler,
	cr *loopruntimev1.ClusterCredential,
	kubeconfigData []byte,
) error {
	return r.reconcileArgoCDClusterSecret(ctx, cr, kubeconfigData)
}

// StubCAFetcher returns a fixed PEM payload and counts invocations.
// Exported for use by envtest specs in package platform_test.
type StubCAFetcher struct {
	ReturnCA  []byte
	callCount atomic.Int64
}

// FetchClusterCA records the call and returns the stub's payload.
func (s *StubCAFetcher) FetchClusterCA(_ context.Context, _ *rest.Config) ([]byte, error) {
	s.callCount.Add(1)
	return s.ReturnCA, nil
}

// CallCount exposes the atomic counter for spec assertions.
func (s *StubCAFetcher) CallCount() int64 {
	return s.callCount.Load()
}

// AlwaysFailFetcher returns a fixed error and counts invocations.
type AlwaysFailFetcher struct {
	Err       error
	callCount atomic.Int64
}

// FetchClusterCA records the call and returns the configured error.
func (f *AlwaysFailFetcher) FetchClusterCA(_ context.Context, _ *rest.Config) ([]byte, error) {
	f.callCount.Add(1)
	return nil, f.Err
}

// CallCount exposes the atomic counter for spec assertions.
func (f *AlwaysFailFetcher) CallCount() int64 {
	return f.callCount.Load()
}
