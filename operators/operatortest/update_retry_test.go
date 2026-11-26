// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package operatortest

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// conflictOnceClient fails the first N Update calls with a 409 Conflict, the
// way a concurrently-reconciling controller does, then delegates.
type conflictOnceClient struct {
	client.Client
	remaining int
	updates   int
}

func (c *conflictOnceClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	c.updates++
	if c.remaining > 0 {
		c.remaining--
		return apierrors.NewConflict(
			schema.GroupResource{Group: "", Resource: "configmaps"},
			obj.GetName(),
			context.Canceled,
		)
	}
	return c.Client.Update(ctx, obj, opts...)
}

func newCM() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "default"},
		Data:       map[string]string{"k": "before"},
	}
}

func fakeClientWith(obj client.Object) client.Client {
	return fake.NewClientBuilder().WithScheme(clientgoscheme.Scheme).WithObjects(obj).Build()
}

// The whole point: an Update that loses the resourceVersion race must be
// retried against a freshly-read object, not surfaced to the caller.
func TestUpdateWithRetry_RetriesOnConflict(t *testing.T) {
	base := fakeClientWith(newCM())
	c := &conflictOnceClient{Client: base, remaining: 2}

	var cm corev1.ConfigMap
	cm.Name, cm.Namespace = "cm", "default"
	err := UpdateWithRetry(context.Background(), c, &cm, func() {
		cm.Data["k"] = "after"
	})
	if err != nil {
		t.Fatalf("expected the conflict to be retried away, got %v", err)
	}
	if c.updates != 3 {
		t.Errorf("expected 3 Update attempts (2 conflicts + 1 success), got %d", c.updates)
	}

	var got corev1.ConfigMap
	if err := base.Get(context.Background(), client.ObjectKey{Name: "cm", Namespace: "default"}, &got); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.Data["k"] != "after" {
		t.Errorf("mutation lost across retries: Data[k]=%q, want %q", got.Data["k"], "after")
	}
}

// A single clean pass must not re-read or re-write more than once.
func TestUpdateWithRetry_NoConflictUpdatesOnce(t *testing.T) {
	base := fakeClientWith(newCM())
	c := &conflictOnceClient{Client: base}

	var cm corev1.ConfigMap
	cm.Name, cm.Namespace = "cm", "default"
	if err := UpdateWithRetry(context.Background(), c, &cm, func() {
		cm.Data["k"] = "after"
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.updates != 1 {
		t.Errorf("expected exactly 1 Update, got %d", c.updates)
	}
}

// Negative case: a non-conflict error is NOT retried away — it reaches the
// caller, so a genuinely broken spec still fails instead of spinning.
func TestUpdateWithRetry_PropagatesNotFound(t *testing.T) {
	base := fake.NewClientBuilder().WithScheme(clientgoscheme.Scheme).Build()

	var cm corev1.ConfigMap
	cm.Name, cm.Namespace = "absent", "default"
	err := UpdateWithRetry(context.Background(), base, &cm, func() {
		cm.Data = map[string]string{"k": "v"}
	})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected the NotFound to propagate, got %v", err)
	}
}
