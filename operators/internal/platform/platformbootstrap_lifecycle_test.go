// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/conditions"
	"github.com/getloopscale/loop-runtime/operators/internal/constants"
)

func bootstrapScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{loopruntimev1.AddToScheme, corev1.AddToScheme} {
		if err := add(s); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func deletingBootstrap() *loopruntimev1.PlatformBootstrap {
	now := metav1.NewTime(time.Now())
	return &loopruntimev1.PlatformBootstrap{ObjectMeta: metav1.ObjectMeta{
		Name: "platform", Namespace: "loop-runtime-system",
		Finalizers:        []string{constants.FinalizerPlatformBootstrap},
		DeletionTimestamp: &now,
	}}
}

func platformTokens(managedBy string) *corev1.Secret {
	return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name: constants.SecretPlatformTokens, Namespace: "loop-runtime-system",
		Labels: map[string]string{constants.LabelManagedBy: managedBy},
	}}
}

func reconcileBootstrap(t *testing.T, c client.Client) error {
	t.Helper()
	r := &PlatformBootstrapReconciler{Client: c}
	_, err := r.Reconcile(context.Background(),
		ctrl.Request{NamespacedName: types.NamespacedName{Name: "platform", Namespace: "loop-runtime-system"}})
	return err
}

func TestPlatformBootstrapDelete_RemovesTheTokenSecretItManages(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(bootstrapScheme(t)).
		WithObjects(deletingBootstrap(), platformTokens(constants.ManagedByPlatformBootstrapController)).Build()

	if err := reconcileBootstrap(t, c); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	err := c.Get(context.Background(),
		client.ObjectKey{Name: constants.SecretPlatformTokens, Namespace: "loop-runtime-system"}, &corev1.Secret{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("the Gitea/ArgoCD token Secret must be deleted with the bootstrap, Get err = %v", err)
	}
}

func TestPlatformBootstrapDelete_LeavesATokenSecretItDoesNotManage(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(bootstrapScheme(t)).
		WithObjects(deletingBootstrap(), platformTokens("someone-else")).Build()

	if err := reconcileBootstrap(t, c); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if err := c.Get(context.Background(),
		client.ObjectKey{Name: constants.SecretPlatformTokens, Namespace: "loop-runtime-system"}, &corev1.Secret{}); err != nil {
		t.Fatalf("a Secret this controller does not manage must survive, Get err = %v", err)
	}
}

func TestPlatformBootstrapDelete_KeepsTheFinalizerWhenTheSecretCannotBeDeleted(t *testing.T) {
	deleteErr := errors.New("apiserver unavailable")
	c := interceptor.NewClient(fake.NewClientBuilder().WithScheme(bootstrapScheme(t)).
		WithObjects(deletingBootstrap(), platformTokens(constants.ManagedByPlatformBootstrapController)).Build(),
		interceptor.Funcs{Delete: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if _, ok := obj.(*corev1.Secret); ok {
				return deleteErr
			}
			return cl.Delete(ctx, obj, opts...)
		}})

	if err := reconcileBootstrap(t, c); !errors.Is(err, deleteErr) {
		t.Fatalf("Reconcile error = %v, want the Secret delete error", err)
	}

	var pb loopruntimev1.PlatformBootstrap
	if err := c.Get(context.Background(), types.NamespacedName{Name: "platform", Namespace: "loop-runtime-system"}, &pb); err != nil {
		t.Fatalf("the PlatformBootstrap must still exist: %v", err)
	}
	if len(pb.Finalizers) == 0 {
		t.Fatal("the finalizer must stay until the token Secret is gone")
	}
}

// A bootstrap that gives up carries the reason in its status, not only in the
// controller log.
func TestPlatformBootstrap_FailedTransitionRecordsTheReason(t *testing.T) {
	pb := &loopruntimev1.PlatformBootstrap{
		ObjectMeta: metav1.ObjectMeta{Name: "platform", Namespace: "loop-runtime-system", Generation: 3},
		Status:     loopruntimev1.PlatformBootstrapStatus{Phase: loopruntimev1.PlatformBootstrapPhaseConfiguringGitea},
	}
	c := fake.NewClientBuilder().WithScheme(bootstrapScheme(t)).WithObjects(pb).
		WithStatusSubresource(&loopruntimev1.PlatformBootstrap{}).Build()
	r := &PlatformBootstrapReconciler{Client: c}

	if _, err := r.transitionToFailed(context.Background(), pb, pb.DeepCopy(), "max retries (5) exhausted: gitea unreachable"); err != nil {
		t.Fatalf("transitionToFailed: %v", err)
	}

	var got loopruntimev1.PlatformBootstrap
	if err := c.Get(context.Background(), types.NamespacedName{Name: "platform", Namespace: "loop-runtime-system"}, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	cond := conditions.GetCondition(got.Status.Conditions, conditions.TypeBootstrapComplete)
	if got.Status.Phase != loopruntimev1.PlatformBootstrapPhaseFailed {
		t.Fatalf("phase = %q, want Failed", got.Status.Phase)
	}
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != conditions.ReasonFailed ||
		cond.Message != "max retries (5) exhausted: gitea unreachable" {
		t.Fatalf("BootstrapComplete condition = %+v, want False/Failed carrying the failure reason", cond)
	}
}
