// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// KubeClient is the domain-facing abstraction over the k8s operations the
// seeds need (architecture.md § Dependency Inversion). Seeds depend on this
// interface; the composition root injects either the real controller-runtime
// client (kubeClient) or a fake client in tests. This is what makes every
// seed hermetically testable without a live cluster.
type KubeClient interface {
	// CreateOrUpdateSecret applies s idempotently: create if absent, else
	// update preserving resourceVersion (sealos Idempotency pattern, blueprint T3).
	// Returns created=true when the Secret did not exist before.
	CreateOrUpdateSecret(ctx context.Context, s *corev1.Secret) (created bool, err error)
	// GetSecret fetches a Secret (used by Verify probes).
	GetSecret(ctx context.Context, namespace, name string) (*corev1.Secret, error)
	// WaitDeploymentReady blocks until the Deployment's readyReplicas ==
	// spec.replicas or the timeout elapses (apimachinery wait, blueprint T4).
	WaitDeploymentReady(ctx context.Context, namespace, name string, timeout time.Duration) error
	// RolloutRestartDeployment triggers a pod rollout by bumping the
	// `kubectl.kubernetes.io/restartedAt` annotation on the Deployment template
	// — same mechanism as `kubectl rollout restart`. Idempotent: two calls in
	// rapid succession produce distinct annotation values.
	//
	// SKIP-friendly: NotFound → nil (returning error) so callers can decide
	// whether a missing Deployment is a hard failure (bash returned exit 1
	// in that case; the sibling seed's rollout is post-apply and non-fatal).
	RolloutRestartDeployment(ctx context.Context, namespace, name string) error
}

// kubeClient is the production KubeClient backed by a controller-runtime client.
type kubeClient struct {
	c client.Client
}

// NewKubeClient wraps a controller-runtime client.Client. The client itself is
// built in the composition root from genericclioptions.ConfigFlags (kubectl-like
// kubeconfig resolution, blueprint T1) — never here.
func NewKubeClient(c client.Client) KubeClient {
	return &kubeClient{c: c}
}

func (k *kubeClient) CreateOrUpdateSecret(ctx context.Context, s *corev1.Secret) (bool, error) {
	err := k.c.Create(ctx, s)
	if err == nil {
		return true, nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return false, fmt.Errorf("create secret %s/%s: %w", s.Namespace, s.Name, err)
	}
	// Exists — update, preserving the live resourceVersion (a merge would 409
	// without it). Get the current object to carry its RV forward.
	var existing corev1.Secret
	if err := k.c.Get(ctx, client.ObjectKey{Namespace: s.Namespace, Name: s.Name}, &existing); err != nil {
		return false, fmt.Errorf("get secret %s/%s for update: %w", s.Namespace, s.Name, err)
	}
	s.ResourceVersion = existing.ResourceVersion
	if err := k.c.Update(ctx, s); err != nil {
		return false, fmt.Errorf("update secret %s/%s: %w", s.Namespace, s.Name, err)
	}
	return false, nil
}

func (k *kubeClient) GetSecret(ctx context.Context, namespace, name string) (*corev1.Secret, error) {
	var s corev1.Secret
	if err := k.c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &s); err != nil {
		return nil, fmt.Errorf("get secret %s/%s: %w", namespace, name, err)
	}
	return &s, nil
}

func (k *kubeClient) RolloutRestartDeployment(ctx context.Context, namespace, name string) error {
	var d appsv1.Deployment
	if err := k.c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &d); err != nil {
		return fmt.Errorf("get deployment %s/%s: %w", namespace, name, err)
	}
	if d.Spec.Template.ObjectMeta.Annotations == nil {
		d.Spec.Template.ObjectMeta.Annotations = map[string]string{}
	}
	d.Spec.Template.ObjectMeta.Annotations["kubectl.kubernetes.io/restartedAt"] =
		time.Now().UTC().Format(time.RFC3339)
	if err := k.c.Update(ctx, &d); err != nil {
		return fmt.Errorf("update deployment %s/%s: %w", namespace, name, err)
	}
	return nil
}

func (k *kubeClient) WaitDeploymentReady(ctx context.Context, namespace, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, timeout, true,
		func(ctx context.Context) (bool, error) {
			var d appsv1.Deployment
			if err := k.c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &d); err != nil {
				if apierrors.IsNotFound(err) {
					return false, nil // not created yet — keep polling
				}
				return false, err
			}
			want := int32(1)
			if d.Spec.Replicas != nil {
				want = *d.Spec.Replicas
			}
			return d.Status.ReadyReplicas == want, nil
		})
}

// secretOf is a small helper for seeds to build a typed Secret with the
// canonical labels. Kept here so every seed produces identically-shaped Secrets.
func secretOf(namespace, name string, data map[string][]byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Data:       data,
		Type:       corev1.SecretTypeOpaque,
	}
}
