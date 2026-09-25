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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// KubeClient is the domain-facing abstraction over the k8s operations the
// seeds need (architecture.md § Dependency Inversion). Seeds depend on this
// interface; the composition root injects the controller-runtime client
// (kubeClient), and tests inject the same type over a fake client. This is
// what makes every seed hermetically testable without a live cluster.
//
// It is composed of roles so a helper can depend on the one it uses. Every
// operation a seed performs is on the port: there is no reach-through to the
// concrete client, which used to make a substitute KubeClient silently skip
// namespace creation and rollouts.
type KubeClient interface {
	SecretStore
	ConfigStore
	Rollouts
	Namespaces
	Services
}

// SecretStore reads and writes Secrets.
type SecretStore interface {
	// CreateOrUpdateSecret applies s idempotently: create if absent, else
	// update preserving resourceVersion. Returns created=true when the Secret
	// did not exist before.
	CreateOrUpdateSecret(ctx context.Context, s *corev1.Secret) (created bool, err error)
	// GetSecret fetches a Secret (used by Verify probes).
	GetSecret(ctx context.Context, namespace, name string) (*corev1.Secret, error)
	// SecretExists reports whether a Secret is present. Returns (false, nil)
	// on NotFound so callers can branch without inspecting a typed error.
	SecretExists(ctx context.Context, namespace, name string) (bool, error)
	// PatchSecretData replaces the given keys' values on the target Secret
	// (Get+Update — preserves resourceVersion and other keys). NotFound-shaped
	// errors are returned verbatim so callers can decide skip vs fail.
	PatchSecretData(ctx context.Context, namespace, name string, patch map[string][]byte) error
}

// ConfigStore reads and patches ConfigMaps.
type ConfigStore interface {
	// GetConfigMap fetches a ConfigMap. NotFound-shaped errors bubble up.
	GetConfigMap(ctx context.Context, namespace, name string) (*corev1.ConfigMap, error)
	// PatchConfigMapData replaces the given data keys on the target ConfigMap
	// (Get+Update — preserves resourceVersion and other keys).
	PatchConfigMapData(ctx context.Context, namespace, name string, patch map[string]string) error
}

// Rollouts restarts workloads the way `kubectl rollout restart` does: by
// stamping `kubectl.kubernetes.io/restartedAt` on the pod template.
type Rollouts interface {
	// RolloutRestartDeployment restarts a Deployment. A missing Deployment is
	// an error; the caller decides whether that is fatal.
	RolloutRestartDeployment(ctx context.Context, namespace, name string) error
	// RolloutRestartDaemonSet restarts a DaemonSet. A missing DaemonSet is
	// nothing to roll out and returns nil.
	RolloutRestartDaemonSet(ctx context.Context, namespace, name string) error
}

// Namespaces ensures a namespace exists before a seed writes into it.
type Namespaces interface {
	// EnsureNamespace creates the namespace when absent; an existing one is
	// left as it is.
	EnsureNamespace(ctx context.Context, name string) error
}

// Services reads and updates Services.
type Services interface {
	// GetService fetches a Service; NotFound-shaped errors bubble up.
	GetService(ctx context.Context, namespace, name string) (*corev1.Service, error)
	// UpdateService writes s back (resourceVersion taken from the read).
	UpdateService(ctx context.Context, s *corev1.Service) error
}

// kubeClient is the production KubeClient backed by a controller-runtime client.
type kubeClient struct {
	c client.Client
	// contextName names the cluster this client talks to. Empty when the caller
	// did not declare it, and then messages read as they always did.
	contextName string
}

// NewKubeClient wraps a controller-runtime client.Client. The client itself is
// built in the composition root from genericclioptions.ConfigFlags (kubectl-like
// kubeconfig resolution, blueprint T1) — never here.
func NewKubeClient(c client.Client) KubeClient {
	return &kubeClient{c: c}
}

// NewKubeClientForContext is the same client, knowing WHICH cluster it talks to.
//
// # Why it exists
//
// B-024, measured 2026-08-10: `loopzero-ops seed patch-kratos` failed with
// `get configmap loopzero-auth/ory-kratos-config: configmaps "ory-kratos-config" not found`,
// and the ConfigMap existed. The seed was talking to another cluster —
// `k3d:secrets:create` switches the active context as a side effect, and seeds
// inherit whatever is active. The message sent people to look for the right
// resource in the wrong cluster, at the cost of debugging time on a system that
// was already broken.
//
// # Why a sibling constructor and not one more parameter
//
// NewKubeClient has 98 callers, 96 of them in tests. Changing its signature
// would churn 96 sites for a benefit that does not involve them — a test with a
// fake client has no kubeconfig context to name. This constructor is opt-in:
// the composition root uses it, and nothing else has to.
func NewKubeClientForContext(c client.Client, contextName string) KubeClient {
	return &kubeClient{c: c, contextName: contextName}
}

// resourceAt names the target of an operation, with the cluster when known.
//
// With no declared context it returns exactly what the message always carried —
// inventing an empty label would be worse than the original.
func (k *kubeClient) resourceAt(namespace, name string) string {
	if k.contextName == "" {
		return namespace + "/" + name
	}
	return namespace + "/" + name + " (context " + k.contextName + ")"
}

func (k *kubeClient) CreateOrUpdateSecret(ctx context.Context, s *corev1.Secret) (bool, error) {
	err := k.c.Create(ctx, s)
	if err == nil {
		return true, nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return false, fmt.Errorf("create secret %s: %w", k.resourceAt(s.Namespace, s.Name), err)
	}
	// Exists — update, preserving the live resourceVersion (a merge would 409
	// without it). Get the current object to carry its RV forward.
	var existing corev1.Secret
	if err := k.c.Get(ctx, client.ObjectKey{Namespace: s.Namespace, Name: s.Name}, &existing); err != nil {
		return false, fmt.Errorf("get secret %s for update: %w", k.resourceAt(s.Namespace, s.Name), err)
	}
	s.ResourceVersion = existing.ResourceVersion
	if err := k.c.Update(ctx, s); err != nil {
		return false, fmt.Errorf("update secret %s: %w", k.resourceAt(s.Namespace, s.Name), err)
	}
	return false, nil
}

func (k *kubeClient) GetSecret(ctx context.Context, namespace, name string) (*corev1.Secret, error) {
	var s corev1.Secret
	if err := k.c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &s); err != nil {
		return nil, fmt.Errorf("get secret %s: %w", k.resourceAt(namespace, name), err)
	}
	return &s, nil
}

func (k *kubeClient) SecretExists(ctx context.Context, namespace, name string) (bool, error) {
	var s corev1.Secret
	if err := k.c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &s); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get secret %s: %w", k.resourceAt(namespace, name), err)
	}
	return true, nil
}

func (k *kubeClient) PatchSecretData(ctx context.Context, namespace, name string, patch map[string][]byte) error {
	var existing corev1.Secret
	if err := k.c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &existing); err != nil {
		return fmt.Errorf("get secret %s: %w", k.resourceAt(namespace, name), err)
	}
	if existing.Data == nil {
		existing.Data = map[string][]byte{}
	}
	for k2, v := range patch {
		existing.Data[k2] = v
	}
	if err := k.c.Update(ctx, &existing); err != nil {
		return fmt.Errorf("update secret %s: %w", k.resourceAt(namespace, name), err)
	}
	return nil
}

func (k *kubeClient) GetConfigMap(ctx context.Context, namespace, name string) (*corev1.ConfigMap, error) {
	var c corev1.ConfigMap
	if err := k.c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &c); err != nil {
		return nil, fmt.Errorf("get configmap %s: %w", k.resourceAt(namespace, name), err)
	}
	return &c, nil
}

func (k *kubeClient) PatchConfigMapData(ctx context.Context, namespace, name string, patch map[string]string) error {
	var existing corev1.ConfigMap
	if err := k.c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &existing); err != nil {
		return fmt.Errorf("get configmap %s: %w", k.resourceAt(namespace, name), err)
	}
	if existing.Data == nil {
		existing.Data = map[string]string{}
	}
	for k2, v := range patch {
		existing.Data[k2] = v
	}
	if err := k.c.Update(ctx, &existing); err != nil {
		return fmt.Errorf("update configmap %s: %w", k.resourceAt(namespace, name), err)
	}
	return nil
}

func (k *kubeClient) RolloutRestartDeployment(ctx context.Context, namespace, name string) error {
	var d appsv1.Deployment
	if err := k.c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &d); err != nil {
		return fmt.Errorf("get deployment %s: %w", k.resourceAt(namespace, name), err)
	}
	if d.Spec.Template.ObjectMeta.Annotations == nil {
		d.Spec.Template.ObjectMeta.Annotations = map[string]string{}
	}
	d.Spec.Template.ObjectMeta.Annotations["kubectl.kubernetes.io/restartedAt"] =
		time.Now().UTC().Format(time.RFC3339)
	if err := k.c.Update(ctx, &d); err != nil {
		return fmt.Errorf("update deployment %s: %w", k.resourceAt(namespace, name), err)
	}
	return nil
}

func (k *kubeClient) RolloutRestartDaemonSet(ctx context.Context, namespace, name string) error {
	var ds appsv1.DaemonSet
	if err := k.c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &ds); err != nil {
		if apierrors.IsNotFound(err) {
			return nil // no DaemonSet, nothing to roll out
		}
		return fmt.Errorf("get daemonset %s: %w", k.resourceAt(namespace, name), err)
	}
	if ds.Spec.Template.Annotations == nil {
		ds.Spec.Template.Annotations = map[string]string{}
	}
	ds.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().UTC().Format(time.RFC3339)
	if err := k.c.Update(ctx, &ds); err != nil {
		return fmt.Errorf("update daemonset %s: %w", k.resourceAt(namespace, name), err)
	}
	return nil
}

func (k *kubeClient) EnsureNamespace(ctx context.Context, name string) error {
	var ns corev1.Namespace
	err := k.c.Get(ctx, client.ObjectKey{Name: name}, &ns)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get namespace %s: %w", name, err)
	}
	created := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if err := k.c.Create(ctx, created); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create namespace %s: %w", name, err)
	}
	return nil
}

func (k *kubeClient) GetService(ctx context.Context, namespace, name string) (*corev1.Service, error) {
	var svc corev1.Service
	if err := k.c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &svc); err != nil {
		return nil, fmt.Errorf("get service %s: %w", k.resourceAt(namespace, name), err)
	}
	return &svc, nil
}

func (k *kubeClient) UpdateService(ctx context.Context, s *corev1.Service) error {
	if err := k.c.Update(ctx, s); err != nil {
		return fmt.Errorf("update service %s: %w", k.resourceAt(s.Namespace, s.Name), err)
	}
	return nil
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
