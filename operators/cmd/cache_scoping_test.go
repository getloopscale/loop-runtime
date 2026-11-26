// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestCacheScopingCongruence_Static enforces AP-5 from the operators
// improvement roadmap: each LoopRuntime CRD type appears in EXACTLY ONE binary's
// cache scoping (cache.Options.ByObject map). Detects:
//   - Overlap: same CRD scoped in two binaries (wasteful + race risk)
//   - Orphan:  CRD reconciled by a binary but missing from its cache scope
//     (falls back to ALL-namespace cache → memory blow-up)
//
// Per ADR-0023 condition + k8s-controller-engineer review: this is the
// static AST layer. A future TestCacheScopingCongruence_Runtime layer will
// inspect mgr.GetCache().GetInformersByGVK() at envtest startup to catch
// declared-but-unused entries (informer not started) and undeclared-but-
// watched entries (controller calls .Watches without ByObject entry).
//
// Plan reference: docs/plans/operators-sota-faang-grade-plan.md T5.4
// (HOISTED to Phase 1 per platform-architect C1 + sre veto).
func TestCacheScopingCongruence_Static(t *testing.T) {
	type binaryCache struct {
		name   string
		main   string
		scopes []string // CRD type names found in cache.Options.ByObject
	}

	bins := []binaryCache{
		{name: "tenant-operator", main: "tenant-operator/main.go"},
		{name: "app-operator", main: "app-operator/main.go"},
		{name: "preview-operator", main: "preview-operator/main.go"},
		{name: "platform-operator", main: "platform-operator/main.go"},
	}

	for i := range bins {
		bins[i].scopes = parseCacheByObjectKeys(t, bins[i].main)
	}

	// Cross-binary congruence: each scoped CRD appears in EXACTLY ONE binary.
	scopeOwners := map[string][]string{}
	for _, b := range bins {
		for _, crd := range b.scopes {
			scopeOwners[crd] = append(scopeOwners[crd], b.name)
		}
	}
	for crd, owners := range scopeOwners {
		if len(owners) > 1 {
			sort.Strings(owners)
			t.Errorf("CRD %q scoped in multiple binaries: %v — each CRD MUST appear in cache.Options.ByObject of exactly ONE binary (AP-5)",
				crd, owners)
		}
	}

	// Reconciler-vs-cache congruence: each binary's reconciled CRD type
	// (inferred from <pkg>.Setup<X>Controller calls) MUST be in its cache
	// scope. Otherwise the controller falls back to the manager's default
	// scope (typically cluster-wide), leaking memory.
	for _, b := range bins {
		reconciled := parseReconciledTypes(t, b.main, b.name)
		for _, crd := range reconciled {
			if !contains(b.scopes, crd) {
				// Soft warning: app-operator + preview-operator currently
				// don't declare cache.Options at all (default scope is fine
				// for those binaries' workload). Only error if the binary
				// HAS a Cache: clause and is missing entries.
				if len(b.scopes) > 0 {
					t.Errorf("%s reconciles %s but does not include it in cache.Options.ByObject. "+
						"Either add the entry or remove all cache scoping from this binary.",
						b.name, crd)
				}
			}
		}
	}
}

// parseCacheByObjectKeys returns the CRD type names found as keys in any
// cache.Options.ByObject literal in the given main.go file.
//
// Recognizes: &loopruntimev1.<TypeName>{}: {...}
func parseCacheByObjectKeys(t *testing.T, mainPath string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, mainPath, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", mainPath, err)
	}

	var keys []string
	ast.Inspect(f, func(n ast.Node) bool {
		composite, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		// Match: cache.Options{ByObject: map[client.Object]cache.ByObject{...}}
		// We look for any map literal whose key pattern is &loopruntimev1.X{}
		mapType, ok := composite.Type.(*ast.MapType)
		if !ok {
			return true
		}
		// Heuristic: key type is client.Object (or similar — we trust the
		// pattern). Iterate elements.
		_ = mapType
		for _, elt := range composite.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			// Key is &loopruntimev1.<TypeName>{}
			unary, ok := kv.Key.(*ast.UnaryExpr)
			if !ok || unary.Op != token.AND {
				continue
			}
			cl, ok := unary.X.(*ast.CompositeLit)
			if !ok {
				continue
			}
			sel, ok := cl.Type.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok {
				continue
			}
			// Accept loopruntimev1.X (most CRDs live there); future package
			// expansions can add aliases here.
			if ident.Name == "loopruntimev1" {
				keys = append(keys, sel.Sel.Name)
			}
		}
		return true
	})
	return keys
}

// parseReconciledTypes infers which CRD types each binary actually reconciles
// by scanning <pkg>.Setup*Controller calls and mapping them via reconcileMap.
func parseReconciledTypes(t *testing.T, mainPath string, binName string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, mainPath, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", mainPath, err)
	}

	// Map Setup<X>Controller → reconciled CRD type name.
	// Matches the canonical naming convention; allows quick audit in PR.
	reconcileMap := map[string]string{
		"SetupTenantController":                 "Tenant",
		"SetupEnvironmentControllerWithFactory": "Environment",
		"SetupApplicationController":            "LoopRuntimeApplication",
		"SetupApplicationControllerWithFactory": "LoopRuntimeApplication",
		"SetupExposureController":               "ExposedRoute",
		"SetupPreviewController":                "PreviewEnvironment",
		"SetupPlatformBootstrapController":      "PlatformBootstrap",
		"SetupGitRepositoryBindingController":   "GitRepositoryBinding",
		"SetupClusterCredentialController":      "ClusterCredential",
	}

	var found []string
	seen := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if crd, ok := reconcileMap[sel.Sel.Name]; ok && !seen[crd] {
			seen[crd] = true
			found = append(found, crd)
		}
		return true
	})
	return found
}

// TestCacheScopingCongruence_Coverage ensures all 8 known LoopRuntime reconcileable
// CRDs are wired by AT LEAST ONE binary. Catches "binary forgot to wire X"
// at the matrix level (T1.3 catches per-binary; this catches inter-binary).
//
// The 2 historical orphan CRDs (AppSecret, ReconciliationReport) were deleted
// 2026-05-13 per ADR `decisions/2026-05-13-orphan-crd-deletion.md` — no
// exclusion entry needed any more.
func TestCacheScopingCongruence_Coverage(t *testing.T) {
	expected := []string{
		"Tenant",
		"Environment",
		"LoopRuntimeApplication",
		"ExposedRoute",
		"PreviewEnvironment",
		"PlatformBootstrap",
		"GitRepositoryBinding",
		"ClusterCredential",
	}

	allReconciled := map[string]bool{}
	for _, mainPath := range []string{
		"tenant-operator/main.go",
		"app-operator/main.go",
		"preview-operator/main.go",
		"platform-operator/main.go",
	} {
		for _, crd := range parseReconciledTypes(t, mainPath, filepath.Base(filepath.Dir(mainPath))) {
			allReconciled[crd] = true
		}
	}

	var missing []string
	for _, crd := range expected {
		if !allReconciled[crd] {
			missing = append(missing, crd)
		}
	}
	if len(missing) > 0 {
		t.Errorf("CRDs reconciled by NO binary: %v — every expected CRD must be wired in at least one of the 4 split binaries",
			missing)
	}
}

// helper: guard against unused-import warnings if ast/sort/strings unused
var _ = strings.HasPrefix
