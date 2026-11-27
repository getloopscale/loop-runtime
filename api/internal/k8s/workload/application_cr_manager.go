// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package workload owns the LoopRuntimeApplication and PreviewEnvironment custom
// resources — the CRs that describe what runs, as opposed to who it runs for.
//
// One of the three homes the B-058 split gave the responsibilities that used to
// share api/internal/k8s because they shared client-go. A file belongs here when it
// reads or writes a workload CR.
package workload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/getloopscale/loop-runtime/api/internal/k8s"
	apiv1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/pkg/naming"
)

// loopRuntimeApplicationsGroupResource synthesizes Conflict errors for RetryOnConflict.
var loopRuntimeApplicationsGroupResource = schema.GroupResource{Group: "runtime.getloopscale.com", Resource: "loopruntimeapplications"}

// LoopRuntimeApplicationInput holds the parameters for creating a LoopRuntimeApplication CR.
type LoopRuntimeApplicationInput struct {
	TenantID    string
	Project     string
	Environment string
	AppName     string
	ServiceName string
	ServicePort int32

	// CorrelationID propagates the originating HTTP request's correlation
	// ID into the CR metadata as the `runtime.getloopscale.com/correlation-id` annotation
	// (audit M08 #10 / §6.C.1). When the operator reconciles this CR, it
	// reads the annotation and injects the value into the reconcile
	// context via correlation.WithID, so audit_log rows + logs emitted
	// by downstream subsystems can trace back to the originating HTTP
	// call. Empty string means "no correlation context available"
	// (background goroutines, recovery loops) and the annotation is
	// omitted instead of stored as empty.
	CorrelationID string
}

// ApplicationCRManager manages LoopRuntimeApplication custom resources on the mgmt cluster.
// It uses the TYPED controller-runtime client (M8): the api is the second Go
// consumer of the operators/api contract module, so the compiler checks every
// CR field at build time (was unstructured map pre-M8).
type ApplicationCRManager struct {
	Client    client.Client
	namespace string
}

// NewApplicationCRManager creates an ApplicationCRManager reusing an existing typed client.
// If namespace is empty, defaults to k8s.ControlPlaneNamespace (loop-runtime-system).
func NewApplicationCRManager(c client.Client, namespace string) *ApplicationCRManager {
	if namespace == "" {
		namespace = k8s.ControlPlaneNamespace
	}
	return &ApplicationCRManager{Client: c, namespace: namespace}
}

// EnsureLoopRuntimeApplication creates or updates a LoopRuntimeApplication CR for the given app.
// It is idempotent: calling it multiple times with the same input is safe.
// The CR name is deterministic: {tenant}-{project}-{app}-{env}.
func (m *ApplicationCRManager) EnsureLoopRuntimeApplication(ctx context.Context, input LoopRuntimeApplicationInput) error {
	crName := loopRuntimeApplicationCRName(input.TenantID, input.Project, input.AppName, input.Environment)

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var existing apiv1.LoopRuntimeApplication
		err := m.Client.Get(ctx, client.ObjectKey{Namespace: m.namespace, Name: crName}, &existing)
		if k8serrors.IsNotFound(err) {
			desired := buildLoopRuntimeApplicationCR(m.namespace, input)
			createErr := m.Client.Create(ctx, desired)
			if k8serrors.IsAlreadyExists(createErr) {
				// Another caller created it between our Get and Create — retry as update.
				return k8serrors.NewConflict(loopRuntimeApplicationsGroupResource, crName, createErr)
			}
			if createErr != nil {
				return fmt.Errorf("k8s: create LoopRuntimeApplication CR %q: %w", crName, createErr)
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("k8s: get LoopRuntimeApplication CR %q: %w", crName, err)
		}

		// CR exists — apply desired spec onto the fetched object, preserving its
		// resourceVersion (carried by `existing`) for optimistic concurrency.
		applyLoopRuntimeApplicationSpec(&existing, input)
		// Audit gap M08 #10 / §6.C.1: refresh the correlation-id annotation on
		// re-deploy so the CR traces back to the LATEST originating request, not
		// the first one (parity with the pre-M8 rebuild-on-update behavior).
		// Unlike that rebuild, this touches ONLY our annotation — operator-set
		// labels/annotations on the existing object are preserved. Empty
		// CorrelationID (background goroutine) leaves the last-known trace in
		// place rather than wiping it.
		if input.CorrelationID != "" {
			if existing.Annotations == nil {
				existing.Annotations = map[string]string{}
			}
			existing.Annotations["runtime.getloopscale.com/correlation-id"] = input.CorrelationID
		}
		if err := m.Client.Update(ctx, &existing); err != nil {
			return err // RetryOnConflict handles Conflict errors automatically
		}
		return nil
	})
}

// maxK8sNameLen is the maximum length for a Kubernetes metadata.name field.
const maxK8sNameLen = 253

// loopRuntimeApplicationCRName computes a deterministic, collision-free name for a LoopRuntimeApplication CR.
// Uses a short hash suffix to avoid ambiguity when component names contain hyphens.
// The name is truncated to 253 chars (K8s limit) if needed, preserving the hash suffix.
// Example: "tn-loop-runtime-my-project-api-production-a1b2c3d4"
func loopRuntimeApplicationCRName(tenant, project, app, env string) string {
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

// buildLoopRuntimeApplicationCR constructs a typed LoopRuntimeApplication CR matching the CRD
// schema defined in operators/api/v1/application_types.go. The compiler now
// enforces the spec shape (was an unstructured map pre-M8).
func buildLoopRuntimeApplicationCR(namespace string, input LoopRuntimeApplicationInput) *apiv1.LoopRuntimeApplication {
	app := &apiv1.LoopRuntimeApplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      loopRuntimeApplicationCRName(input.TenantID, input.Project, input.AppName, input.Environment),
			Namespace: namespace,
			Labels: map[string]string{
				naming.LabelTenant:      input.TenantID,
				naming.LabelProject:     input.Project,
				naming.LabelEnvironment: input.Environment,
				naming.LabelApp:         input.AppName,
			},
		},
	}
	// Audit gap M08 #10 / §6.C.1: correlation_id annotation when present.
	// Operator reconciles read this back into the ctx via correlation.WithID
	// so the trace persists across the API→K8s→controller boundary.
	if input.CorrelationID != "" {
		app.Annotations = map[string]string{
			"runtime.getloopscale.com/correlation-id": input.CorrelationID,
		}
	}
	applyLoopRuntimeApplicationSpec(app, input)
	return app
}

// applyLoopRuntimeApplicationSpec writes the desired spec onto a LoopRuntimeApplication object.
// Used by both create (fresh object) and update (fetched object, resourceVersion
// preserved). Labels/annotations are set only at build time (create path).
func applyLoopRuntimeApplicationSpec(app *apiv1.LoopRuntimeApplication, input LoopRuntimeApplicationInput) {
	port := input.ServicePort
	if port == 0 {
		port = 80
	}
	app.Spec.TenantRef = input.TenantID
	app.Spec.Project = input.Project
	app.Spec.Environment = input.Environment
	// The operator composes the routed hostname from it (naming.AppHostname).
	app.Spec.AppName = input.AppName
	app.Spec.ServiceName = input.ServiceName
	app.Spec.ServicePort = port
	app.Spec.Replicas = 1
}
