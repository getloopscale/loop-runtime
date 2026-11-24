// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// CHG-2026-04-30-INFRA-NORMALIZE / Phase 2 condition C16.
// Source: docs/decisions/MEET-INFRA-NORMALIZE-T3-operator-split.md
// Source: docs/decisions/operator-split-invariants.md (I-1)
//
// Pins that the split operators that handle namespace-scoped CRDs
// (Tenant/Environment in tenant-operator; ClusterCredential/
// GitRepoBinding/PlatformBootstrap in platform-operator) carry the
// cache.Options scoping per ADR-0018 v2 C9.
//
// Static-source check rather than envtest because:
// (a) running envtest requires KUBEBUILDER_ASSETS + ~30s setup per test,
//     CI cost-prohibitive for a contract this thin;
// (b) the contract IS source-level: if the cache.Options block is
//     present with the right ByObject keys, controller-runtime applies
//     the scope. Live verification belongs in the cutover-canary phase
//     (T-13/T-14 already SKIP cloud-pendente).
//
// If a future refactor moves the cache.Options to a helper, this test
// must be updated to chase the helper. That's intentional — pinning
// the source string forces conscious review of cache changes.

package unit

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var whitespaceRun = regexp.MustCompile(`\s+`)

// sourceClauses reads a Go file with every run of whitespace made one space, so a clause
// is found however gofmt aligns the declarations around it.
func sourceClauses(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s missing: %v", path, err)
	}
	return whitespaceRun.ReplaceAllString(string(data), " ")
}

func TestCacheScoping_TenantOperatorScopesTenantAndEnvironment(t *testing.T) {
	text := sourceClauses(t, filepath.Join(repoRoot(), "operators", "cmd", "tenant-operator", "main.go"))

	for _, must := range []string{
		"platformCRDNamespaces",
		"constants.NamespacePlatformSystem",
		"func managerCacheOptions() (cache.Options, error)",
		"Cache:                  cacheOpts",
		"ByObject: map[client.Object]cache.ByObject",
		"&loopzerov1.Tenant{}",
		"&loopzerov1.Environment{}",
		"&corev1.Secret{}:      secretScope",
	} {
		if !strings.Contains(text, whitespaceRun.ReplaceAllString(must, " ")) {
			t.Errorf("tenant-operator main.go missing C16/I-1 cache scoping clause: %s", must)
		}
	}
}

func TestCacheScoping_PlatformOperatorScopesPlatformCRDs(t *testing.T) {
	text := sourceClauses(t, filepath.Join(repoRoot(), "operators", "cmd", "platform-operator", "main.go"))

	for _, must := range []string{
		"platformCRDNamespaces",
		"func managerCacheOptions(argoCDNamespace string) (cache.Options, error)",
		"Cache:                  cacheOpts",
		"&corev1.Secret{}:               secretScope",
		"&loopzerov1.PlatformBootstrap{}",
		"&loopzerov1.GitRepositoryBinding{}",
		"&loopzerov1.ClusterCredential{}",
	} {
		if !strings.Contains(text, whitespaceRun.ReplaceAllString(must, " ")) {
			t.Errorf("platform-operator main.go missing C16/I-1 cache scoping clause: %s", must)
		}
	}
}

func TestCacheScoping_AppOperatorDoesNOTScope_WorkloadCRDs(t *testing.T) {
	// Per ADR-0018 v2 C9: LoopZeroApplication / PreviewEnvironment /
	// ExposedRoute MUST remain cluster-wide because they are deployed
	// per-tenant in `loopzero-tenant-{id}` namespaces. Restricting their
	// cache to a single namespace would BREAK reconciliation. This
	// test is the negative pin — fails if a future commit accidentally
	// applies cache scoping to these CRDs in the app or preview
	// operators.
	for _, op := range []string{"app-operator", "preview-operator"} {
		path := filepath.Join(repoRoot(), "operators", "cmd", op, "main.go")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s main.go missing: %v", op, err)
		}
		text := string(data)

		// If we see ANY ByObject entry for the workload CRDs, that's
		// a regression we want to catch.
		for _, forbidden := range []string{
			"&loopzerov1.LoopZeroApplication{}:      {Namespaces:",
			"&loopzerov1.PreviewEnvironment{}:    {Namespaces:",
			"&loopzerov1.ExposedRoute{}:          {Namespaces:",
		} {
			if strings.Contains(text, forbidden) {
				t.Errorf("%s applies forbidden namespace scoping to workload CRD: %s — ADR-0018 C9 mandates cluster-wide", op, forbidden)
			}
		}
	}
}
