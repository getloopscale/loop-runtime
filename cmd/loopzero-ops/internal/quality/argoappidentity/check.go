// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package argoappidentity implements the `argo-app-identity` quality gate.
//
// # WHAT IT GUARDS
//
// The name of an ArgoCD Application is derived in exactly one place —
// pkg/naming.ArgoAppName(tenant, project, env) — and never rebuilt with fmt.Sprintf.
//
// # WHY A GATE
//
// The canonical function existed, was tested, and had ZERO production callers, while
// ten sites across two modules rebuilt the name by hand as `{tenant}-{env}`, dropping
// the project. Two projects of one tenant in one environment therefore collided on a
// single Application with conflicting destinations (issue #271; measured at 1519 writes
// in 5 minutes on app-dev).
//
// Counting them by hand did not converge: the first sweep found 7, a second found the
// two inside pruneOrphanPreviewApps, and a third found the one in the promote path. The
// two in the prune were the dangerous ones — that function DELETES every Application
// outside the set it builds, and the delete cascades through
// resources-finalizer.argocd.argoproj.io into the tenant's workload. Had the emit side
// been renamed without them, the operator would have deleted, in cascade, exactly what
// it had just created.
//
// A defect that a careful human undercounted twice is a defect that belongs to a gate.
//
// # LIMITS, STATED HONESTLY
//
// This is textual matching, not AST analysis. A caller that assembles the name in two
// steps, or through a helper, escapes it. The gate lowers the odds of an eleventh
// occurrence; it does not make one impossible.
package argoappidentity

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// scanRoots are the trees where an ArgoCD Application name can legitimately be needed.
var scanRoots = []string{
	"operators/internal/tenant",
	"api/internal/services/release",
}

// adHocRe matches `fmt.Sprintf("%s-%s", <x>.TenantID|<x>.Name, <y>)` — the shape that
// drops the project. The tenant term is what makes it an Application name rather than
// any other two-part identifier.
var adHocRe = regexp.MustCompile(
	`fmt\.Sprintf\(\s*"%s-%s"\s*,\s*[A-Za-z_][A-Za-z0-9_.]*\.(TenantID|Name)\s*,`)

// appVarRe requires the result to land in something named like an app, so that
// Environment CR names (envCRName) and release-scoped app names are not accused.
var appVarRe = regexp.MustCompile(`(?i)\b(argo)?app(name)?\b\s*(:?=)`)

// Finding is one gate violation.
type Finding struct {
	File    string
	Line    int
	Snippet string
}

// Report is the gate result.
type Report struct {
	Findings []Finding
	Scanned  int
}

// OK reports whether the gate passed.
func (r Report) OK() bool { return len(r.Findings) == 0 }

// Message renders the remediation hint for a finding.
func (f Finding) Message() string {
	return fmt.Sprintf("%s:%d rebuilds an ArgoCD Application name by hand: %s\n"+
		"    use naming.ArgoAppName(tenant, project, env) — omitting the project makes two "+
		"projects of one tenant collide on a single Application (#271)",
		f.File, f.Line, f.Snippet)
}

// Check runs the gate against repoRoot.
func Check(repoRoot string) (Report, error) {
	var rep Report

	for _, root := range scanRoots {
		dir := filepath.Join(repoRoot, root)
		if _, err := os.Stat(dir); err != nil {
			continue // tree absent in this checkout — not this gate's business
		}
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			raw, err := os.ReadFile(path) //nolint:gosec // repo-relative walk
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			rep.Scanned++
			rel, _ := filepath.Rel(repoRoot, path)
			for i, line := range strings.Split(string(raw), "\n") {
				trimmed := strings.TrimSpace(line)
				// A comment naming the old shape is history, not a code path. Flagging it
				// would train people to delete the record of why something changed.
				if strings.HasPrefix(trimmed, "//") {
					continue
				}
				if adHocRe.MatchString(line) && appVarRe.MatchString(line) {
					rep.Findings = append(rep.Findings, Finding{
						File: rel, Line: i + 1, Snippet: trimmed,
					})
				}
			}
			return nil
		})
		if err != nil {
			return rep, fmt.Errorf("walk %s: %w", dir, err)
		}
	}
	return rep, nil
}
