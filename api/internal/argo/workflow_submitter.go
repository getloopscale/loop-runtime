// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package argo provides real Argo Workflows integration for build pipeline execution.
package argo

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/loopzero/loopzero/api/internal/services"
)

const (
	submitMaxRetries    = 3
	submitBaseDelay     = 1 * time.Second
)

var workflowGVR = schema.GroupVersionResource{
	Group:    "argoproj.io",
	Version:  "v1alpha1",
	Resource: "workflows",
}

// WorkflowSubmitterConfig holds configuration for connecting to the build cluster.
type WorkflowSubmitterConfig struct {
	Kubeconfig string // Path to kubeconfig file
	Context    string // Kube context (e.g., "k3d-loopzero-build")

	// Infrastructure parameters passed to every workflow via the WorkflowBuilder.
	// C-01 fix: CallbackURL/CallbackSecret removed. No more HTTP callback.
	// BLOCKER-2 fix: GiteaURL/GiteaToken removed. Clone CWT gets credentials
	// from K8s Secret (loopzero-gitea-credentials) via envFrom.
	RegistryURL      string // e.g., "k3d-loopzero-registry:5000"
	RegistryInsecure bool   // true for local dev registries without TLS
	BaseDomain       string // e.g., "loopzero.local" — base domain for app hostnames
	UseKaniko        bool   // Build engine: true=Kaniko (legacy), false=BuildKit. Migration flag.
	// MinIO credentials are injected via K8s Secret (envFrom) in CWTs, not via workflow params.
}

// WorkflowSubmitter implements services.WorkflowSubmitter using real Argo Workflows.
// It uses the WorkflowBuilder to generate self-contained Workflow CRs with
// ClusterWorkflowTemplate refs for reusable steps and inline templates for
// language-specific steps.
//
// MOD-2: The submitter is namespace-agnostic. Each workflow runs in the tenant's
// own build namespace (e.g., "build-acme"), passed via WorkflowSpec.Namespace.
type WorkflowSubmitter struct {
	client  dynamic.Interface
	builder *WorkflowBuilder
}

// NewWorkflowSubmitter creates a WorkflowSubmitter connected to the build cluster.
func NewWorkflowSubmitter(cfg WorkflowSubmitterConfig) (*WorkflowSubmitter, error) {
	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: cfg.Kubeconfig}
	overrides := &clientcmd.ConfigOverrides{}
	if cfg.Context != "" {
		overrides.CurrentContext = cfg.Context
	}

	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("argo: build cluster config: %w", err)
	}

	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("argo: dynamic client: %w", err)
	}

	builder := NewWorkflowBuilder(InfraParams{
		RegistryURL:      cfg.RegistryURL,
		RegistryInsecure: cfg.RegistryInsecure,
		BaseDomain:       cfg.BaseDomain,
		UseKaniko:        cfg.UseKaniko,
	})

	return &WorkflowSubmitter{
		client:  client,
		builder: builder,
	}, nil
}

// Submit generates a self-contained Workflow CR via the WorkflowBuilder and creates it
// in the build cluster with retry for transient errors.
func (s *WorkflowSubmitter) Submit(ctx context.Context, spec services.WorkflowSpec) (string, error) {
	wf, err := s.builder.Build(spec)
	if err != nil {
		return "", fmt.Errorf("argo: build workflow: %w", err)
	}

	wf.Namespace = spec.Namespace

	wfObject, err := wf.ToDict()
	if err != nil {
		return "", fmt.Errorf("argo: serialize workflow: %w", err)
	}

	workflow := &unstructured.Unstructured{Object: wfObject}

	var lastErr error
	for attempt := 0; attempt <= submitMaxRetries; attempt++ {
		if attempt > 0 {
			delay := submitBaseDelay * time.Duration(1<<(attempt-1))
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(delay):
			}
		}

		created, err := s.client.Resource(workflowGVR).Namespace(spec.Namespace).Create(ctx, workflow, metav1.CreateOptions{})
		if err == nil {
			return created.GetName(), nil
		}

		lastErr = err

		// Only retry on transient server errors (5xx), not client errors (4xx)
		if k8serrors.IsServerTimeout(err) || k8serrors.IsServiceUnavailable(err) ||
			k8serrors.IsTooManyRequests(err) || k8serrors.IsInternalError(err) {
			continue
		}

		// Non-retryable error — fail immediately
		return "", fmt.Errorf("argo: create workflow: %w", err)
	}

	return "", fmt.Errorf("argo: create workflow after %d retries: %w", submitMaxRetries, lastErr)
}

// Cancel stops a running workflow by setting its shutdown strategy.
func (s *WorkflowSubmitter) Cancel(ctx context.Context, namespace, workflowID string) error {
	patch := []byte(`{"spec":{"shutdown":"Stop"}}`)
	_, err := s.client.Resource(workflowGVR).Namespace(namespace).Patch(
		ctx, workflowID, "application/merge-patch+json", patch, metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("argo: cancel workflow %s: %w", workflowID, err)
	}
	return nil
}

// GetStatus checks the external state of a workflow in the build cluster.
// Returns (status, nil) if the workflow exists, or (nil, nil) if not found.
func (s *WorkflowSubmitter) GetStatus(ctx context.Context, namespace, workflowID string) (*services.WorkflowStatus, error) {
	wf, err := s.client.Resource(workflowGVR).Namespace(namespace).Get(ctx, workflowID, metav1.GetOptions{})
	if err != nil {
		// Not found — workflow was cleaned up or never existed
		return nil, nil
	}

	phase, _, _ := unstructured.NestedString(wf.Object, "status", "phase")
	return &services.WorkflowStatus{Phase: phase}, nil
}
