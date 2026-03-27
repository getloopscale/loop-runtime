// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argocd

import (
	"testing"
)

func TestBuildApplicationObject_ManualSyncPolicy(t *testing.T) {
	// The Application CR created by EnsureApplication must use manual sync
	// (automated: nil), matching the spec, Helm template, and infra test.
	// Auto-sync (selfHeal/prune) creates race conditions with the API's
	// manual POST /sync trigger. See MEET-2026-03-27-012.

	spec := ApplicationSpec{
		Name:           "acme-production",
		Namespace:      "argocd",
		TenantID:       "acme",
		RepoURL:        "http://gitea:3000/acme/gitops.git",
		TargetRevision: "HEAD",
		Path:           "production",
		DestinationURL: "https://workload:6443",
		DestinationNS:  "loopzero-acme-production",
	}

	app := buildApplicationObject(spec)

	// Extract syncPolicy from the unstructured object
	appSpec, ok := app.Object["spec"].(map[string]interface{})
	if !ok {
		t.Fatal("missing spec in Application object")
	}

	syncPolicy, ok := appSpec["syncPolicy"].(map[string]interface{})
	if !ok {
		t.Fatal("missing syncPolicy in Application spec")
	}

	// automated must be nil/absent (manual sync — API controls timing)
	automated, exists := syncPolicy["automated"]
	if exists && automated != nil {
		t.Errorf("syncPolicy.automated must be nil (manual sync), got %v", automated)
	}

	// syncOptions must include PrunePropagationPolicy=foreground and PruneLast=true
	syncOptions, ok := syncPolicy["syncOptions"].([]interface{})
	if !ok {
		t.Fatal("missing syncOptions in syncPolicy")
	}

	// CreateNamespace must be false (TenantController creates namespaces)
	foundCreateNS := false
	for _, opt := range syncOptions {
		if s, ok := opt.(string); ok && s == "CreateNamespace=false" {
			foundCreateNS = true
		}
	}
	if !foundCreateNS {
		t.Error("syncOptions must include CreateNamespace=false (namespace created by TenantController)")
	}
}

func TestBuildApplicationObject_Labels(t *testing.T) {
	spec := ApplicationSpec{
		Name:           "acme-production",
		Namespace:      "argocd",
		TenantID:       "acme",
		RepoURL:        "http://gitea:3000/acme/gitops.git",
		TargetRevision: "HEAD",
		Path:           "production",
		DestinationURL: "https://workload:6443",
		DestinationNS:  "loopzero-acme-production",
	}

	app := buildApplicationObject(spec)

	metadata, ok := app.Object["metadata"].(map[string]interface{})
	if !ok {
		t.Fatal("missing metadata")
	}
	labels, ok := metadata["labels"].(map[string]interface{})
	if !ok {
		t.Fatal("missing labels")
	}

	if labels["loopzero.io/tenant"] != "acme" {
		t.Errorf("expected tenant label 'acme', got %v", labels["loopzero.io/tenant"])
	}
	if labels["loopzero.io/managed-by"] != "tenant-controller" {
		t.Errorf("expected managed-by label 'tenant-controller', got %v", labels["loopzero.io/managed-by"])
	}
}
