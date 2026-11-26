// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package argoappidentity implements the `argo-app-identity` quality gate.
//
// # WHAT IT GUARDS
//
// The identity of a per-environment resource is derived in exactly one place —
// pkg/naming.ArgoAppName / pkg/naming.EnvironmentCRName, both (tenant, project, env) —
// and never rebuilt with fmt.Sprintf.
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
// The Environment CR had the identical shape and a worse consequence (#273): its name
// dropped the project while its content (spec.project, spec.namespace.name) kept it, so
// two projects of one tenant collapsed onto a single CR through CreateOrUpdate and the
// loser silently got no namespace, RBAC or NetworkPolicy at all. Measured on app-dev:
// 5 of 9 declared (project, environment) tuples had no Environment CR. Deleting one of
// those CRs deletes the tenant's namespace, so its `desired` set is a delete path too.
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

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

// scanRoots are the trees where an ArgoCD Application name can legitimately be needed.
var scanRoots = []string{
	"operators/internal/tenant",
	"api/internal/release",
}

// adHocRe matches `fmt.Sprintf("%s-%s", <x>.TenantID|<x>.Name, <y>)` — the shape that
// drops the project. The tenant term is what makes it an Application name rather than
// any other two-part identifier.
var adHocRe = regexp.MustCompile(
	`fmt\.Sprintf\(\s*"%s-%s"\s*,\s*[A-Za-z_][A-Za-z0-9_.]*\.(TenantID|Name)\s*,`)

// idVarRe requires the result to land in something named like a resource identity —
// an ArgoCD Application or an Environment CR — so release-scoped app names are not
// accused. Both identities dropped the project and both fed a delete path (#271, #273).
var idVarRe = regexp.MustCompile(
	`(?i)(\b(argo)?app(name)?\b\s*:?=|\benvcrname\b\s*:?=|\bdesired\[)`)

// envCRRe tells the two identities apart so the remediation names the right function.
var envCRRe = regexp.MustCompile(`(?i)(\benvcrname\b|\bdesired\[)`)

const (
	kindApplication   = "application"
	kindEnvironmentCR = "environment-cr"
)

// Finding is one gate violation.
type Finding struct {
	File    string
	Line    int
	Snippet string
	// Kind selects the remediation, because pointing at the wrong canonical function
	// would just get the same defect rewritten a different way.
	Kind string // "application" | "environment-cr"
}

// Report is the gate result.
type Report struct {
	Findings []Finding
	Scanned  int
}

// OK reports whether the gate passed.
func (r Report) OK() bool { return len(r.Findings) == 0 }

// Verdict is Fail when any finding was reported and Pass otherwise.
func (r Report) Verdict() verdict.Verdict { return verdict.Of(r.Findings) }

// Message renders the remediation hint for a finding.
func (f Finding) Message() string {
	fn, consequence, issue := "naming.ArgoAppName",
		"two projects of one tenant collide on a single Application with conflicting destinations",
		"#271"
	if f.Kind == kindEnvironmentCR {
		fn, consequence, issue = "naming.EnvironmentCRName",
			"two projects of one tenant collapse onto a single Environment CR and the loser "+
				"silently gets no namespace, RBAC or NetworkPolicy",
			"#273"
	}
	return fmt.Sprintf("%s:%d rebuilds a per-environment resource identity by hand: %s\n"+
		"    use %s(tenant, project, env) — omitting the project makes %s (%s)",
		f.File, f.Line, f.Snippet, fn, consequence, issue)
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
				if adHocRe.MatchString(line) && idVarRe.MatchString(line) {
					kind := kindApplication
					if envCRRe.MatchString(line) {
						kind = kindEnvironmentCR
					}
					rep.Findings = append(rep.Findings, Finding{
						File: rel, Line: i + 1, Snippet: trimmed, Kind: kind,
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
