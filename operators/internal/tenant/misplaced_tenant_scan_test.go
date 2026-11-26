// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenant

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	loopzerov1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/constants"
	"github.com/getloopscale/loop-runtime/operators/internal/metrics"
)

func tenantIn(namespace, name string) *loopzerov1.Tenant {
	return &loopzerov1.Tenant{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
}

// ADR-0018 C2/C5: a Tenant CR outside loopzero-system is invisible to the scoped
// cache, so an uncached cluster-wide scan is what surfaces it — through the
// gauge the MisplacedTenantDetected alert queries.
func TestMisplacedTenantScan_CountsTenantsOutsideTheCanonicalNamespace(t *testing.T) {
	metrics.MisplacedTenantTotal.Reset()
	reader := fake.NewClientBuilder().WithScheme(cellScheme(t)).WithObjects(
		tenantIn(constants.NamespacePlatformSystem, "tn-ok"),
		tenantIn("default", "tn-stray-a"),
		tenantIn("default", "tn-stray-b"),
		tenantIn("team-x", "tn-stray-c"),
	).Build()

	if err := scanMisplacedTenants(context.Background(), reader); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if got := testutil.ToFloat64(metrics.MisplacedTenantTotal.WithLabelValues("default")); got != 2 {
		t.Fatalf("misplaced in default = %v, want 2", got)
	}
	if got := testutil.ToFloat64(metrics.MisplacedTenantTotal.WithLabelValues("team-x")); got != 1 {
		t.Fatalf("misplaced in team-x = %v, want 1", got)
	}
	if n := testutil.CollectAndCount(metrics.MisplacedTenantTotal); n != 2 {
		t.Fatalf("series = %d, want only the two misplaced namespaces (loopzero-system is never reported)", n)
	}
}

func TestMisplacedTenantScan_ClearsANamespaceThatWasFixed(t *testing.T) {
	metrics.MisplacedTenantTotal.Reset()
	metrics.MisplacedTenantTotal.WithLabelValues("default").Set(3)
	reader := fake.NewClientBuilder().WithScheme(cellScheme(t)).
		WithObjects(tenantIn(constants.NamespacePlatformSystem, "tn-ok")).Build()

	if err := scanMisplacedTenants(context.Background(), reader); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if n := testutil.CollectAndCount(metrics.MisplacedTenantTotal); n != 0 {
		t.Fatalf("series = %d, want none once no Tenant is misplaced", n)
	}
}

func TestMisplacedTenantScan_ReturnsTheListErrorAndKeepsThePreviousReading(t *testing.T) {
	metrics.MisplacedTenantTotal.Reset()
	metrics.MisplacedTenantTotal.WithLabelValues("default").Set(1)
	listErr := errors.New("forbidden")
	reader := interceptor.NewClient(fake.NewClientBuilder().WithScheme(cellScheme(t)).Build(), interceptor.Funcs{
		List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error { return listErr },
	})

	if err := scanMisplacedTenants(context.Background(), reader); !errors.Is(err, listErr) {
		t.Fatalf("scan error = %v, want the list error", err)
	}
	if got := testutil.ToFloat64(metrics.MisplacedTenantTotal.WithLabelValues("default")); got != 1 {
		t.Fatalf("a failed scan must not zero the gauge (that would clear a firing alert), got %v", got)
	}
}
