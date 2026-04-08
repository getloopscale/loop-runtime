// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package k8s

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

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

// maxK8sNameLen is the maximum length for a Kubernetes metadata.name field.
const maxK8sNameLen = 253

// loopzeroApplicationCRName computes a deterministic, collision-free name for a LoopZeroApplication CR.
// Uses a short hash suffix to avoid ambiguity when component names contain hyphens.
// The name is truncated to 253 chars (K8s limit) if needed, preserving the hash suffix.
// Example: "tn-loopzero-my-project-api-production-a1b2c3d4"
func loopzeroApplicationCRName(tenant, project, app, env string) string {
	// The hash input uses pipe separators to guarantee uniqueness even when
	// components contain hyphens (e.g., project="my-app" vs project="my", app="app").
	hashInput := fmt.Sprintf("%s|%s|%s|%s", tenant, project, app, env)
	h := sha256.Sum256([]byte(hashInput))
	suffix := hex.EncodeToString(h[:4]) // 8 hex chars = 4 bytes, collision-resistant

	prefix := fmt.Sprintf("%s-%s-%s-%s", tenant, project, app, env)
	// Truncate prefix if full name would exceed K8s limit (prefix + "-" + suffix).
	if len(prefix)+1+len(suffix) > maxK8sNameLen {
		prefix = prefix[:maxK8sNameLen-1-len(suffix)]
		// Trim trailing hyphens to keep the name a valid RFC 1123 subdomain.
		prefix = strings.TrimRight(prefix, "-")
	}
	return prefix + "-" + suffix
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
