// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package conditions

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	return s
}

func newCM(name, ns string, data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Data:       data,
	}
}

func cmKey(name, ns string) client.ObjectKey {
	return client.ObjectKey{Name: name, Namespace: ns}
}

// Test_UpdateStatusWithRetry_Success — happy path: no conflict, mutate
// runs once, Status().Update succeeds. (T2.3 listed case 1.)
func Test_UpdateStatusWithRetry_Success(t *testing.T) {
	cm := newCM("cm1", "default", map[string]string{"v": "0"})
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(cm).
		WithStatusSubresource(&corev1.ConfigMap{}).Build()

	var calls int32
	err := UpdateStatusWithRetry(context.Background(), c, cmKey("cm1", "default"),
		func() *corev1.ConfigMap { return &corev1.ConfigMap{} },
		func(_ *corev1.ConfigMap) error {
			atomic.AddInt32(&calls, 1)
			return nil
		})
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("mutate called %d times, want 1", got)
	}
}

// trackingClient counts Get/Status calls and can inject a conflict on
// the Nth call to Status().Update().
type trackingClient struct {
	client.Client
	getCalls         int32
	statusUpdateCalls int32
	conflictUntil     int32 // return Conflict on Status calls 1..N
}

func (t *trackingClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	atomic.AddInt32(&t.getCalls, 1)
	return t.Client.Get(ctx, key, obj, opts...)
}

func (t *trackingClient) Status() client.SubResourceWriter {
	return &trackingStatus{parent: t, inner: t.Client.Status()}
}

type trackingStatus struct {
	parent *trackingClient
	inner  client.SubResourceWriter
}

func (s *trackingStatus) Create(ctx context.Context, obj client.Object, sub client.Object, opts ...client.SubResourceCreateOption) error {
	return s.inner.Create(ctx, obj, sub, opts...)
}
func (s *trackingStatus) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	n := atomic.AddInt32(&s.parent.statusUpdateCalls, 1)
	if n <= s.parent.conflictUntil {
		return apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, obj.GetName(), errors.New("simulated conflict"))
	}
	return s.inner.Update(ctx, obj, opts...)
}
func (s *trackingStatus) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	return s.inner.Patch(ctx, obj, patch, opts...)
}
func (s *trackingStatus) Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
	return s.inner.Apply(ctx, obj, opts...)
}

// Test_UpdateStatusWithRetry_ConflictThenSuccess — Status().Update returns
// Conflict on the first attempt then succeeds. Helper retries; mutate runs
// twice (once per attempt) and helper returns nil. (T2.3 listed case 2.)
func Test_UpdateStatusWithRetry_ConflictThenSuccess(t *testing.T) {
	cm := newCM("cm1", "default", nil)
	inner := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(cm).
		WithStatusSubresource(&corev1.ConfigMap{}).Build()
	c := &trackingClient{Client: inner, conflictUntil: 1}

	var mutateCalls int32
	err := UpdateStatusWithRetry(context.Background(), c, cmKey("cm1", "default"),
		func() *corev1.ConfigMap { return &corev1.ConfigMap{} },
		func(_ *corev1.ConfigMap) error {
			atomic.AddInt32(&mutateCalls, 1)
			return nil
		})
	if err != nil {
		t.Fatalf("expected nil err after retry, got %v", err)
	}
	if got := atomic.LoadInt32(&mutateCalls); got < 2 {
		t.Errorf("mutate called %d times, want >= 2 (1 conflict + 1 success)", got)
	}
	if got := atomic.LoadInt32(&c.statusUpdateCalls); got < 2 {
		t.Errorf("Status.Update called %d times, want >= 2", got)
	}
}

// Test_UpdateStatusWithRetry_PersistentConflict — every attempt returns
// Conflict; helper exhausts the retry budget and returns the last error.
// (T2.3 listed case 3.)
func Test_UpdateStatusWithRetry_PersistentConflict(t *testing.T) {
	cm := newCM("cm1", "default", nil)
	inner := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(cm).
		WithStatusSubresource(&corev1.ConfigMap{}).Build()
	c := &trackingClient{Client: inner, conflictUntil: 100} // always conflict

	err := UpdateStatusWithRetry(context.Background(), c, cmKey("cm1", "default"),
		func() *corev1.ConfigMap { return &corev1.ConfigMap{} },
		func(_ *corev1.ConfigMap) error { return nil })
	if err == nil {
		t.Fatalf("expected conflict err after retry budget exhausted, got nil")
	}
	if !apierrors.IsConflict(err) {
		t.Errorf("expected IsConflict error, got %v", err)
	}
}

// Test_UpdateStatusWithRetry_NotFound — Get returns NotFound; helper does
// NOT call mutate, returns the error so caller can decide if terminal.
// (T2.3 listed case 4.)
func Test_UpdateStatusWithRetry_NotFound(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithStatusSubresource(&corev1.ConfigMap{}).Build()

	var mutateCalls int32
	err := UpdateStatusWithRetry(context.Background(), c, cmKey("missing", "default"),
		func() *corev1.ConfigMap { return &corev1.ConfigMap{} },
		func(_ *corev1.ConfigMap) error {
			atomic.AddInt32(&mutateCalls, 1)
			return nil
		})
	if err == nil {
		t.Fatalf("expected NotFound err, got nil")
	}
	if !apierrors.IsNotFound(err) {
		t.Errorf("expected IsNotFound, got %v", err)
	}
	if got := atomic.LoadInt32(&mutateCalls); got != 0 {
		t.Errorf("mutate called %d times on NotFound; should be 0", got)
	}
}

// Test_UpdateStatusWithRetry_MutateModifiesSpec_IsRejected — guard test
// per ADR-0024 condition #2 (k8s-controller-engineer review). Mutate that
// modifies labels (a metadata footgun for Status().Update) MUST be caught
// and rejected with ErrSpecMutation before Status().Update runs.
func Test_UpdateStatusWithRetry_MutateModifiesSpec_IsRejected(t *testing.T) {
	cm := newCM("cm1", "default", nil)
	cm.Labels = map[string]string{"original": "yes"}
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(cm).
		WithStatusSubresource(&corev1.ConfigMap{}).Build()

	err := UpdateStatusWithRetry(context.Background(), c, cmKey("cm1", "default"),
		func() *corev1.ConfigMap { return &corev1.ConfigMap{} },
		func(o *corev1.ConfigMap) error {
			// Footgun: editing labels is dropped by Status().Update.
			o.Labels["mutated"] = "yes"
			return nil
		})
	if err == nil {
		t.Fatalf("expected ErrSpecMutation, got nil")
	}
	if !errors.Is(err, ErrSpecMutation) {
		t.Errorf("expected errors.Is(err, ErrSpecMutation), got %v", err)
	}
}

// Test_UpdateStatusWithRetry_ContextCanceledMidLoop — ctx.Done() between
// retry attempts MUST short-circuit the loop. Per ADR-0024 condition #2
// (manager shutdown safety).
func Test_UpdateStatusWithRetry_ContextCanceledMidLoop(t *testing.T) {
	cm := newCM("cm1", "default", nil)
	inner := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(cm).
		WithStatusSubresource(&corev1.ConfigMap{}).Build()
	c := &trackingClient{Client: inner, conflictUntil: 100} // would normally retry

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel BEFORE first attempt

	err := UpdateStatusWithRetry(ctx, c, cmKey("cm1", "default"),
		func() *corev1.ConfigMap { return &corev1.ConfigMap{} },
		func(_ *corev1.ConfigMap) error { return nil })
	if err == nil {
		t.Fatalf("expected context.Canceled-related err, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected errors.Is(err, context.Canceled), got %v", err)
	}
}

// Test_UpdateStatusWithRetry_GenericNonConflictError_NoRetry — when the
// Status().Update returns a non-Conflict error (e.g., Unauthorized), the
// helper MUST return immediately without retry. Per ADR-0024 condition #2.
func Test_UpdateStatusWithRetry_GenericNonConflictError_NoRetry(t *testing.T) {
	cm := newCM("cm1", "default", nil)
	inner := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(cm).
		WithStatusSubresource(&corev1.ConfigMap{}).Build()
	c := &errorInjectingClient{Client: inner, statusErr: apierrors.NewUnauthorized("denied")}

	err := UpdateStatusWithRetry(context.Background(), c, cmKey("cm1", "default"),
		func() *corev1.ConfigMap { return &corev1.ConfigMap{} },
		func(_ *corev1.ConfigMap) error { return nil })
	if err == nil {
		t.Fatalf("expected Unauthorized err, got nil")
	}
	if !apierrors.IsUnauthorized(err) {
		t.Errorf("expected IsUnauthorized, got %v", err)
	}
	if c.statusUpdateCalls != 1 {
		t.Errorf("Status.Update called %d times on non-Conflict; should be 1 (no retry)", c.statusUpdateCalls)
	}
}

type errorInjectingClient struct {
	client.Client
	statusErr         error
	statusUpdateCalls int
}

func (e *errorInjectingClient) Status() client.SubResourceWriter {
	return &errorInjectingStatus{parent: e, inner: e.Client.Status()}
}

type errorInjectingStatus struct {
	parent *errorInjectingClient
	inner  client.SubResourceWriter
}

func (s *errorInjectingStatus) Create(ctx context.Context, obj client.Object, sub client.Object, opts ...client.SubResourceCreateOption) error {
	return s.inner.Create(ctx, obj, sub, opts...)
}
func (s *errorInjectingStatus) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	s.parent.statusUpdateCalls++
	if s.parent.statusErr != nil {
		return s.parent.statusErr
	}
	return s.inner.Update(ctx, obj, opts...)
}
func (s *errorInjectingStatus) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	return s.inner.Patch(ctx, obj, patch, opts...)
}
func (s *errorInjectingStatus) Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
	return s.inner.Apply(ctx, obj, opts...)
}

// Test_UpdateStatusWithRetry_MutateError_NoRetry — mutate returning an
// error propagates immediately without retry.
func Test_UpdateStatusWithRetry_MutateError_NoRetry(t *testing.T) {
	cm := newCM("cm1", "default", nil)
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(cm).
		WithStatusSubresource(&corev1.ConfigMap{}).Build()

	mutateErr := errors.New("intentional")
	var calls int32
	err := UpdateStatusWithRetry(context.Background(), c, cmKey("cm1", "default"),
		func() *corev1.ConfigMap { return &corev1.ConfigMap{} },
		func(_ *corev1.ConfigMap) error {
			atomic.AddInt32(&calls, 1)
			return mutateErr
		})
	if !errors.Is(err, mutateErr) {
		t.Errorf("expected mutate error to propagate, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("mutate called %d times on its own error; should be 1 (no retry)", got)
	}
}

// Test_statusRetryBackoff_BoundedDuration — sanity check that the
// backoff config matches ADR-0024 (max ~3.3s worst case via Cap=2s).
func Test_statusRetryBackoff_BoundedDuration(t *testing.T) {
	if statusRetryBackoff.Steps < 3 || statusRetryBackoff.Steps > 10 {
		t.Errorf("statusRetryBackoff.Steps = %d, want 3-10 (FAANG SLA bar)", statusRetryBackoff.Steps)
	}
	if statusRetryBackoff.Cap > 5*time.Second {
		t.Errorf("statusRetryBackoff.Cap = %v, want <= 5s (worker saturation prevention per sre-engineer)", statusRetryBackoff.Cap)
	}
}
