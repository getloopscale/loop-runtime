// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package k8s

import (
	"context"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"
)

var loopzeroApplicationGVR = schema.GroupVersionResource{
	Group:    "loopzero.io",
	Version:  "v1",
	Resource: "loopzeroapplications",
}

// LoopZeroApplicationInput holds the parameters for creating a LoopZeroApplication CR.
type LoopZeroApplicationInput struct {
	TenantID    string
	Project     string
	Environment string
	AppName     string
	ServiceName string
	ServicePort int32
}

// ApplicationCRManager manages LoopZeroApplication custom resources on the mgmt cluster.
// It follows the same pattern as TenantCRManager: dynamic client + unstructured objects.
type ApplicationCRManager struct {
	client    dynamic.Interface
	namespace string
}

// NewApplicationCRManager creates an ApplicationCRManager reusing an existing dynamic client.
// If namespace is empty, defaults to tenantNamespace (platform-system in prod, loopzero-system in dev).
func NewApplicationCRManager(client dynamic.Interface, namespace string) *ApplicationCRManager {
	if namespace == "" {
		namespace = tenantNamespace
	}
	return &ApplicationCRManager{client: client, namespace: namespace}
}

// EnsureLoopZeroApplication creates or updates a LoopZeroApplication CR for the given app.
// It is idempotent: calling it multiple times with the same input is safe.
// The CR name is deterministic: {tenant}-{project}-{app}-{env}.
func (m *ApplicationCRManager) EnsureLoopZeroApplication(ctx context.Context, input LoopZeroApplicationInput) error {
	resource := m.client.Resource(loopzeroApplicationGVR).Namespace(m.namespace)
	crName := loopzeroApplicationCRName(input.TenantID, input.Project, input.AppName, input.Environment)

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing, err := resource.Get(ctx, crName, metav1.GetOptions{})
		if k8serrors.IsNotFound(err) {
			desired := buildLoopZeroApplicationCR(m.namespace, input)
			_, createErr := resource.Create(ctx, desired, metav1.CreateOptions{})
			if k8serrors.IsAlreadyExists(createErr) {
				return k8serrors.NewConflict(
					schema.GroupResource{Group: "loopzero.io", Resource: "loopzeroapplications"},
					crName, createErr,
				)
			}
			if createErr != nil {
				return fmt.Errorf("k8s: create LoopZeroApplication CR %q: %w", crName, createErr)
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("k8s: get LoopZeroApplication CR %q: %w", crName, err)
		}

		// Update spec, preserving resourceVersion for optimistic concurrency.
		desired := buildLoopZeroApplicationCR(m.namespace, input)
		desired.SetResourceVersion(existing.GetResourceVersion())
		_, err = resource.Update(ctx, desired, metav1.UpdateOptions{})
		return err
	})
}

// loopzeroApplicationCRName computes the deterministic name for a LoopZeroApplication CR.
func loopzeroApplicationCRName(tenant, project, app, env string) string {
	return fmt.Sprintf("%s-%s-%s-%s", tenant, project, app, env)
}

// buildLoopZeroApplicationCR constructs an unstructured LoopZeroApplication matching the CRD
// defined in operators/api/v1/application_types.go.
func buildLoopZeroApplicationCR(namespace string, input LoopZeroApplicationInput) *unstructured.Unstructured {
	port := int64(input.ServicePort)
	if port == 0 {
		port = 80
	}

	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "loopzero.io/v1",
			"kind":       "LoopZeroApplication",
			"metadata": map[string]interface{}{
				"name":      loopzeroApplicationCRName(input.TenantID, input.Project, input.AppName, input.Environment),
				"namespace": namespace,
				"labels": map[string]interface{}{
					"loopzero.io/tenant":      input.TenantID,
					"loopzero.io/project":     input.Project,
					"loopzero.io/environment": input.Environment,
					"loopzero.io/app":         input.AppName,
				},
			},
			"spec": map[string]interface{}{
				"tenantRef":   input.TenantID,
				"project":     input.Project,
				"environment": input.Environment,
				"serviceName": input.ServiceName,
				"servicePort": port,
				"replicas":    int64(1),
			},
		},
	}
}
