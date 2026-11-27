// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package unit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// loadYAMLFile reads and parses a YAML file into a generic map.
func loadYAMLFile(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read file %s: %v", path, err)
	}
	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("invalid YAML in %s: %v", path, err)
	}
	return doc
}

func applicationTemplatePath() string {
	return filepath.Join(helmDir(), "argocd", "templates", "tenant-app-template.yaml")
}

func appProjectTemplatePath() string {
	return filepath.Join(helmDir(), "argocd", "templates", "tenant-appproject-template.yaml")
}

// --- Application template tests ---

func TestApplicationTemplateIsValidYAML(t *testing.T) {
	doc := loadYAMLFile(t, applicationTemplatePath())

	apiVersion, ok := doc["apiVersion"].(string)
	if !ok || apiVersion != "argoproj.io/v1alpha1" {
		t.Errorf("expected apiVersion argoproj.io/v1alpha1, got %v", doc["apiVersion"])
	}

	kind, ok := doc["kind"].(string)
	if !ok || kind != "Application" {
		t.Errorf("expected kind Application, got %v", doc["kind"])
	}
}

func TestApplicationTemplateRendersCorrectStructure(t *testing.T) {
	doc := loadYAMLFile(t, applicationTemplatePath())

	// Must have metadata with labels
	metadata, ok := doc["metadata"].(map[string]interface{})
	if !ok {
		t.Fatal("missing metadata")
	}

	labels, ok := metadata["labels"].(map[string]interface{})
	if !ok {
		t.Fatal("missing metadata.labels")
	}

	requiredLabels := []string{"runtime.getloopscale.com/tenant", "runtime.getloopscale.com/project", "runtime.getloopscale.com/environment"}
	for _, label := range requiredLabels {
		if _, ok := labels[label]; !ok {
			t.Errorf("missing required label: %s", label)
		}
	}

	// Must have finalizers for resource cleanup
	finalizers, ok := metadata["finalizers"].([]interface{})
	if !ok || len(finalizers) == 0 {
		t.Error("missing metadata.finalizers — required for ArgoCD resource cleanup")
	}

	// Must have spec
	spec, ok := doc["spec"].(map[string]interface{})
	if !ok {
		t.Fatal("missing spec")
	}

	// Must have source
	source, ok := spec["source"].(map[string]interface{})
	if !ok {
		t.Fatal("missing spec.source")
	}

	// Source must have repoURL, targetRevision, path
	for _, field := range []string{"repoURL", "targetRevision", "path"} {
		if _, ok := source[field]; !ok {
			t.Errorf("missing spec.source.%s", field)
		}
	}

	// Must have destination
	destination, ok := spec["destination"].(map[string]interface{})
	if !ok {
		t.Fatal("missing spec.destination")
	}
	for _, field := range []string{"server", "namespace"} {
		if _, ok := destination[field]; !ok {
			t.Errorf("missing spec.destination.%s", field)
		}
	}
}

func TestApplicationTemplateSyncPolicyIsManual(t *testing.T) {
	doc := loadYAMLFile(t, applicationTemplatePath())

	spec, ok := doc["spec"].(map[string]interface{})
	if !ok {
		t.Fatal("missing spec")
	}

	syncPolicy, ok := spec["syncPolicy"].(map[string]interface{})
	if !ok {
		t.Fatal("missing spec.syncPolicy")
	}

	// automated must be null/nil (manual sync — API controls when to sync)
	automated, exists := syncPolicy["automated"]
	if exists && automated != nil {
		t.Errorf("spec.syncPolicy.automated must be null (manual sync) — got %v", automated)
	}

	// syncOptions must include CreateNamespace=false
	syncOptions, ok := syncPolicy["syncOptions"].([]interface{})
	if !ok {
		t.Fatal("missing spec.syncPolicy.syncOptions")
	}

	foundCreateNS := false
	for _, opt := range syncOptions {
		optStr, ok := opt.(string)
		if !ok {
			continue
		}
		if optStr == "CreateNamespace=false" {
			foundCreateNS = true
		}
	}
	if !foundCreateNS {
		t.Error("spec.syncPolicy.syncOptions must include CreateNamespace=false (namespace created by TenantController)")
	}
}

func TestApplicationTemplateParameterized(t *testing.T) {
	data, err := os.ReadFile(applicationTemplatePath())
	if err != nil {
		t.Fatalf("cannot read file: %v", err)
	}

	content := string(data)

	// Template must use placeholder variables for tenant, project, environment
	placeholders := []string{"TENANT_PLACEHOLDER", "PROJECT_PLACEHOLDER", "ENVIRONMENT_PLACEHOLDER"}
	for _, ph := range placeholders {
		if !strings.Contains(content, ph) {
			t.Errorf("template must contain parameterized placeholder %q", ph)
		}
	}
}

func TestApplicationTemplateIgnoresDifferencesForHPA(t *testing.T) {
	doc := loadYAMLFile(t, applicationTemplatePath())

	spec, ok := doc["spec"].(map[string]interface{})
	if !ok {
		t.Fatal("missing spec")
	}

	ignoreDiffs, ok := spec["ignoreDifferences"].([]interface{})
	if !ok || len(ignoreDiffs) == 0 {
		t.Fatal("missing spec.ignoreDifferences — required to ignore HPA-managed replicas")
	}

	// At least one entry should ignore /spec/replicas on Deployments
	found := false
	for _, diff := range ignoreDiffs {
		diffMap, ok := diff.(map[string]interface{})
		if !ok {
			continue
		}
		kind, _ := diffMap["kind"].(string)
		if kind != "Deployment" {
			continue
		}
		pointers, ok := diffMap["jsonPointers"].([]interface{})
		if !ok {
			continue
		}
		for _, p := range pointers {
			if pStr, ok := p.(string); ok && pStr == "/spec/replicas" {
				found = true
			}
		}
	}
	if !found {
		t.Error("spec.ignoreDifferences must include /spec/replicas for Deployment (HPA may change this)")
	}
}

// --- AppProject template tests ---

func TestAppProjectTemplateIsValidYAML(t *testing.T) {
	doc := loadYAMLFile(t, appProjectTemplatePath())

	apiVersion, ok := doc["apiVersion"].(string)
	if !ok || apiVersion != "argoproj.io/v1alpha1" {
		t.Errorf("expected apiVersion argoproj.io/v1alpha1, got %v", doc["apiVersion"])
	}

	kind, ok := doc["kind"].(string)
	if !ok || kind != "AppProject" {
		t.Errorf("expected kind AppProject, got %v", doc["kind"])
	}
}

func TestAppProjectNamespaceRestrictedToTenant(t *testing.T) {
	doc := loadYAMLFile(t, appProjectTemplatePath())

	spec, ok := doc["spec"].(map[string]interface{})
	if !ok {
		t.Fatal("missing spec")
	}

	destinations, ok := spec["destinations"].([]interface{})
	if !ok || len(destinations) == 0 {
		t.Fatal("missing spec.destinations")
	}

	// All destinations must restrict namespace to {tenant}-* pattern
	for i, dest := range destinations {
		destMap, ok := dest.(map[string]interface{})
		if !ok {
			t.Errorf("destination %d is not a map", i)
			continue
		}
		ns, ok := destMap["namespace"].(string)
		if !ok {
			t.Errorf("destination %d missing namespace", i)
			continue
		}
		// Namespace must contain the tenant placeholder followed by a wildcard
		if !strings.Contains(ns, "TENANT_PLACEHOLDER") || !strings.Contains(ns, "*") {
			t.Errorf("destination %d namespace %q must restrict to tenant pattern with wildcard", i, ns)
		}
	}
}

func TestAppProjectSourceReposRestrictedToTenant(t *testing.T) {
	doc := loadYAMLFile(t, appProjectTemplatePath())

	spec, ok := doc["spec"].(map[string]interface{})
	if !ok {
		t.Fatal("missing spec")
	}

	sourceRepos, ok := spec["sourceRepos"].([]interface{})
	if !ok || len(sourceRepos) == 0 {
		t.Fatal("missing spec.sourceRepos")
	}

	// All source repos must be scoped to the tenant's Gitea org
	for i, repo := range sourceRepos {
		repoStr, ok := repo.(string)
		if !ok {
			t.Errorf("sourceRepos[%d] is not a string", i)
			continue
		}
		if !strings.Contains(repoStr, "TENANT_PLACEHOLDER") {
			t.Errorf("sourceRepos[%d] %q must be scoped to tenant Gitea org", i, repoStr)
		}
	}
}

func TestAppProjectNoClusterScopedResources(t *testing.T) {
	doc := loadYAMLFile(t, appProjectTemplatePath())

	spec, ok := doc["spec"].(map[string]interface{})
	if !ok {
		t.Fatal("missing spec")
	}

	whitelist, ok := spec["clusterResourceWhitelist"].([]interface{})
	if !ok {
		t.Fatal("missing spec.clusterResourceWhitelist — must be explicitly empty")
	}

	if len(whitelist) != 0 {
		t.Errorf("spec.clusterResourceWhitelist must be empty (tenants cannot create cluster-scoped resources), got %d entries", len(whitelist))
	}
}

func TestAppProjectNamespaceResourceBlacklist(t *testing.T) {
	doc := loadYAMLFile(t, appProjectTemplatePath())

	spec, ok := doc["spec"].(map[string]interface{})
	if !ok {
		t.Fatal("missing spec")
	}

	blacklist, ok := spec["namespaceResourceBlacklist"].([]interface{})
	if !ok || len(blacklist) == 0 {
		t.Fatal("missing spec.namespaceResourceBlacklist — must block ResourceQuota, LimitRange, NetworkPolicy")
	}

	// Collect blacklisted kinds
	blacklistedKinds := make(map[string]bool)
	for _, item := range blacklist {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		kind, _ := itemMap["kind"].(string)
		if kind != "" {
			blacklistedKinds[kind] = true
		}
	}

	requiredBlacklist := []string{"ResourceQuota", "LimitRange", "NetworkPolicy"}
	for _, kind := range requiredBlacklist {
		if !blacklistedKinds[kind] {
			t.Errorf("spec.namespaceResourceBlacklist must include %s (managed by TenantController)", kind)
		}
	}
}

func TestAppProjectTemplateParameterized(t *testing.T) {
	data, err := os.ReadFile(appProjectTemplatePath())
	if err != nil {
		t.Fatalf("cannot read file: %v", err)
	}

	content := string(data)

	if !strings.Contains(content, "TENANT_PLACEHOLDER") {
		t.Error("AppProject template must contain TENANT_PLACEHOLDER for parameterization")
	}
}
